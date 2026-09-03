package coding

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
)

// CurrentSessionVersion matches pi's session file format version.
const CurrentSessionVersion = 3

// DefaultSessionDir returns the per-cwd session directory under the agent dir,
// using pi's safe-path encoding (--<cwd with separators as dashes>--).
// It returns "" when AgentDir() does — with no home there is nowhere global to
// put sessions, and the old relative fallback wrote them into whatever repo the
// process was run in.
func DefaultSessionDir(cwd string) string {
	agentDir := AgentDir()
	if agentDir == "" {
		return ""
	}
	resolved, _ := filepath.Abs(cwd)
	return filepath.Join(agentDir, "sessions", "--"+encodeCwdSafePath(resolved)+"--")
}

// encodeCwdSafePath mirrors pi's safe-path encoding (session-manager.ts:441):
// strip exactly ONE leading separator (replace(/^[/\\]/, "")), then replace
// every '/'/'\'/':' with '-'.
func encodeCwdSafePath(resolved string) string {
	if len(resolved) > 0 && (resolved[0] == '/' || resolved[0] == '\\') {
		resolved = resolved[1:]
	}
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(resolved)
}

func genID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const (
	// maxUUIDv7Timestamp is pi's MAX_UUID_V7_TIMESTAMP: the largest millisecond
	// timestamp a v7 UUID's 48-bit time field can carry.
	maxUUIDv7Timestamp = int64(0xffffffffffff)
	// maxUUIDv7Sequence is pi's MAX_SEQUENCE: the ceiling of the 41-bit counter
	// packed into bytes 6-11.
	maxUUIDv7Sequence = uint64(1)<<41 - 1
)

// uuidv7Now mirrors pi's uuidv7() (packages/ai/src/utils/uuid.ts) with no
// argument: a time-ordered v7 UUID (version nibble 7, RFC-4122 variant) in
// canonical 8-4-4-4-12 form, stamped with the wall clock — never earlier than
// the last clock-stamped id, so a clock that steps backwards cannot un-order
// ids.
//
// It returns an error exactly where pi throws its RangeError and no id can be
// produced: a system clock outside [1970-01-01, +10889] (unrepresentable in 48
// bits — a machine whose RTC has been reset to a pre-1970 date reaches this)
// or the process's 41-bit sequence exhausted. Both mean the generator cannot
// mint a valid, unique id at all; the alternative would be silently emitting
// duplicate session ids. The counter is seeded with 40 random bits, so
// exhaustion needs upwards of 2^40 ids from one process; restarting the
// process reseeds it.
func uuidv7Now() (string, error) {
	return nextUUIDv7(nil)
}

// mustUUIDv7 is the panicking convenience form of uuidv7Now, for the one caller
// that cannot yet propagate an error (compaction.go's per-request routing
// session id). Every caller that returns an error uses uuidv7Now instead, and
// so should any new one.
func mustUUIDv7() string {
	id, err := uuidv7Now()
	if err != nil {
		panic(err)
	}
	return id
}

// uuidv7At mirrors pi's uuidv7(timestampMs): a v7 UUID carrying timestampMs
// verbatim, for "follower" ids that must sort at a known point in time. Unlike
// uuidv7Now it does not advance — or read — the clock-stamped high-water mark,
// so a follower id cannot drag ordinary ids forward.
func uuidv7At(timestampMs int64) (string, error) {
	return nextUUIDv7(&timestampMs)
}

// nextUUIDv7 is the shared generator. A nil timestampMs is pi's
// `timestampMs === undefined`: read the clock, and floor the result at (and
// store it as) the clock-stamped high-water mark.
//
// Go has no BigInt, and none is needed: the sequence is 41 bits and the
// timestamp 48, both of which fit a uint64/int64 exactly.
func nextUUIDv7(timestampMs *int64) (string, error) {
	clockStamped := timestampMs == nil
	requested := uuidNow()
	if !clockStamped {
		requested = *timestampMs
	}
	if requested < 0 || requested > maxUUIDv7Timestamp {
		return "", fmt.Errorf(
			"UUIDv7 timestamp must be an integer between 0 and %d, got %d: pass a Unix time in milliseconds (a value outside that range does not fit a v7 UUID's 48-bit time field)",
			maxUUIDv7Timestamp, requested)
	}

	var random [16]byte
	uuidRandom(random[:])
	// Seed 40 of the 41 counter bits from random bytes 1-5, leaving at least
	// 2^40 ordered values before exhaustion. Those bytes are then overwritten by
	// the timestamp; the tail bytes 12-15 stay fresh on every id.
	seed := uint64(random[1])<<32 |
		uint64(random[2])<<24 |
		uint64(random[3])<<16 |
		uint64(random[4])<<8 |
		uint64(random[5])

	seq, effective, err := reserveUUIDv7(seed, requested, clockStamped)
	if err != nil {
		return "", err
	}

	var b [16]byte
	ts := uint64(effective)
	for i := 0; i < 6; i++ {
		b[i] = byte(ts >> ((5 - i) * 8))
	}
	b[6] = 0x70 | byte((seq>>37)&0x0f)
	b[7] = byte((seq >> 29) & 0xff)
	b[8] = 0x80 | byte((seq>>23)&0x3f)
	b[9] = byte((seq >> 15) & 0xff)
	b[10] = byte((seq >> 7) & 0xff)
	b[11] = byte((seq&0x7f)<<1) | (random[11] & 0x01)
	copy(b[12:], random[12:])

	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// reserveUUIDv7 advances the process-wide generator state under uuidMu and
// hands back the sequence value and effective timestamp for one id. seed
// supplies the 40 random bits used the first (and only) time the counter is
// seeded. Keeping the whole critical section in one function means the unlock
// is a single defer rather than one per exit path.
func reserveUUIDv7(seed uint64, requestedMs int64, clockStamped bool) (seq uint64, effectiveMs int64, err error) {
	uuidMu.Lock()
	defer uuidMu.Unlock()

	effectiveMs = requestedMs
	if clockStamped {
		if effectiveMs < uuidLastOrdinaryTimestamp {
			effectiveMs = uuidLastOrdinaryTimestamp
		}
		uuidLastOrdinaryTimestamp = effectiveMs
	}
	switch {
	case !uuidSequenceSeeded:
		uuidSequence = seed
		uuidSequenceSeeded = true
	case uuidSequence == maxUUIDv7Sequence:
		return 0, 0, fmt.Errorf(
			"UUIDv7 generator sequence exhausted after %d ids: restart the process to reseed the counter",
			maxUUIDv7Sequence)
	default:
		uuidSequence++
	}
	return uuidSequence, effectiveMs, nil
}

var (
	uuidMu sync.Mutex
	// uuidLastOrdinaryTimestamp is pi's lastOrdinaryTimestamp, starting below
	// every valid timestamp so the first clock-stamped id is never floored.
	uuidLastOrdinaryTimestamp int64 = -1
	// uuidSequence is pi's module-scoped `sequence`; uuidSequenceSeeded stands
	// in for its `undefined` state, which a uint64 cannot express (0 is a
	// reachable seed).
	uuidSequence       uint64
	uuidSequenceSeeded bool

	// uuidNow and uuidRandom are the generator's clock and randomness. They are
	// vars because that is the only seam pi's own tests use: uuid.test.ts drives
	// the clock-rollback floor with vi.setSystemTime and pins which random byte
	// reaches which uuid byte with vi.stubGlobal("crypto", ...). Production code
	// must never reassign them.
	uuidNow    = func() int64 { return time.Now().UnixMilli() }
	uuidRandom = func(b []byte) { _, _ = rand.Read(b) }
)

// randomUUIDv4 returns a canonical random (version 4, RFC-4122 variant) UUID —
// node's randomUUID(), which is what pi's session-manager mints.
func randomUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = 0x40 | b[6]&0x0f
	b[8] = 0x80 | b[8]&0x3f
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// genEntryID returns an 8-hex-char entry id for which taken reports false
// (pi's generateId, session-manager.ts:221-228: randomUUID().slice(0,8),
// collision-checked against a `{ has(id) }`, with a full randomUUID() as the
// after-100-collisions fallback). pi slices a v4 UUID (fully random first 8
// chars); genID() yields the same 8-random-hex-char shape, so we reuse it.
func genEntryID(taken func(id string) bool) string {
	for i := 0; i < 100; i++ {
		id := genID()
		if !taken(id) {
			return id
		}
	}
	return randomUUIDv4()
}

// readSessionEntries decodes a session file's JSONL lines into raw entries —
// malformed lines skipped, as pi's parseSessionEntryLine does — and brings them
// to CurrentSessionVersion in memory (pi migrateToCurrentVersion,
// session-manager.ts:281-291). migrated reports whether any migration ran; pi
// rewrites the file whenever it did (_loadEntries → _rewriteFile), which is the
// caller's decision here because the read-only loaders never write.
//
// Numbers are kept as json.Number so a rewrite reproduces every value verbatim
// rather than through float64. A line is one JSON value and nothing else:
// JSON.parse throws on trailing bytes, so a line carrying two entries fused
// together (an unterminated tail plus a later append, issue #8345) is dropped
// whole rather than resurrecting its first entry as an orphan.
func readSessionEntries(data []byte) (entries []map[string]any, migrated bool) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber()
		var entry map[string]any
		if dec.Decode(&entry) != nil || entry == nil || dec.InputOffset() != int64(len(line)) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, migrateSessionEntries(entries)
}

// sessionHeader returns the first session entry, pi's
// `entries.find((e) => e.type === "session")`, or nil when the file has none.
func sessionHeader(entries []map[string]any) map[string]any {
	for _, e := range entries {
		if e["type"] == "session" {
			return e
		}
	}
	return nil
}

// migrateSessionEntries runs pi's migrations in place and reports whether any
// applied. As in pi's _loadEntries the migration is gated on a session header
// (a headerless entry list is not a session file and is left alone), the
// header's missing version means 1 (`header?.version ?? 1`: v1 files predate
// the field), and files at or beyond CurrentSessionVersion are untouched.
func migrateSessionEntries(entries []map[string]any) bool {
	header := sessionHeader(entries)
	if header == nil {
		return false
	}
	version := 1
	if v, ok := header["version"].(json.Number); ok {
		if n, err := v.Int64(); err == nil {
			version = int(n)
		}
	}
	if version >= CurrentSessionVersion {
		return false
	}
	if version < 2 {
		migrateSessionV1ToV2(entries)
	}
	if version < 3 {
		migrateSessionV2ToV3(entries)
	}
	return true
}

// migrateSessionV1ToV2 ports pi's migrateV1ToV2 (session-manager.ts:231-257):
// v1 files are linear and carry no id/parentId, so every entry gets a fresh
// entry id chained to the one before it, and a compaction's firstKeptEntryIndex
// — an index into the file including the header — becomes the id of that entry.
func migrateSessionV1ToV2(entries []map[string]any) {
	taken := map[string]bool{}
	var prevID any // nil: the first entry's parentId is null
	for _, e := range entries {
		if e["type"] == "session" {
			e["version"] = 2
			continue
		}
		id := genEntryID(func(id string) bool { return taken[id] })
		taken[id] = true
		e["id"] = id
		e["parentId"] = prevID
		prevID = id

		if e["type"] != "compaction" {
			continue
		}
		idx, ok := e["firstKeptEntryIndex"].(json.Number)
		if !ok {
			continue
		}
		if i, err := idx.Int64(); err == nil && i >= 0 && i < int64(len(entries)) {
			if target := entries[i]; target["type"] != "session" {
				if targetID, ok := target["id"].(string); ok {
					e["firstKeptEntryId"] = targetID
				}
			}
		}
		delete(e, "firstKeptEntryIndex")
	}
}

// migrateSessionV2ToV3 ports pi's migrateV2ToV3 (session-manager.ts:260-275):
// the hookMessage role was renamed custom.
func migrateSessionV2ToV3(entries []map[string]any) {
	for _, e := range entries {
		if e["type"] == "session" {
			e["version"] = 3
			continue
		}
		if e["type"] != "message" {
			continue
		}
		if msg, ok := e["message"].(map[string]any); ok && msg["role"] == "hookMessage" {
			msg["role"] = "custom"
		}
	}
}

// rewriteSessionFile replaces path's contents with entries, one JSON object per
// line (pi _rewriteFile, session-manager.ts:990-1000: open "w", write every
// entry). It runs only after a migration, so the file is already known to be a
// pi session.
func rewriteSessionFile(path string, entries []map[string]any) error {
	var buf strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("cannot re-encode migrated session entry for %s: %w", path, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("cannot rewrite migrated session file: %w (check that %s is writable, or resume a different session)", err, path)
	}
	return nil
}

func isoNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// SessionInfo summarizes a stored session file.
type SessionInfo struct {
	Path      string
	ID        string
	Cwd       string
	Timestamp string
	Messages  int
}

// SessionRecorder appends an agent transcript to a JSONL session file, matching
// pi's append-only format (header + linear message/model/thinking entries).
//
// Writes are withheld until the first assistant message is recorded (pi
// _persist): a started-but-unused session leaves no file on disk. Pending
// entries are buffered and flushed atomically on the first assistant message.
type SessionRecorder struct {
	mu       sync.Mutex
	path     string
	id       string
	lastID   string
	file     *os.File
	byID     map[string]bool
	pending  []map[string]any
	flushed  bool
	hasAsst  bool
	createFn func() (*os.File, error)
}

// StartSession creates a new session for cwd and buffers the header plus an
// initial model entry. The session file is created lazily on the first recorded
// assistant message.
//
// When thinkingLevel is given, a thinking_level_change entry is recorded after
// the model_change, matching pi's createAgentSession for new sessions
// (sdk.ts:362-373: appendModelChange then appendThinkingLevelChange; the
// thinking entry is written even when there is no model). The variadic
// parameter keeps existing callers source-compatible.
func StartSession(cwd string, model *ai.Model, thinkingLevel ...string) (*SessionRecorder, error) {
	dir := DefaultSessionDir(cwd)
	if dir == "" {
		return nil, fmt.Errorf("cannot locate the agent directory: no home directory is set, so there is nowhere to store sessions. Set HOME (or USERPROFILE on Windows), or pass an explicit session directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	id, err := uuidv7Now()
	if err != nil {
		return nil, err
	}
	ts := isoNow()
	fileTS := strings.NewReplacer(":", "-", ".", "-").Replace(ts)
	path := filepath.Join(dir, fileTS+"_"+id+".jsonl")
	resolved, _ := filepath.Abs(cwd)
	r := &SessionRecorder{
		path: path,
		id:   id,
		byID: map[string]bool{},
		createFn: func() (*os.File, error) {
			// pi opens the first flush with "wx" (O_EXCL): never clobber an
			// existing file (session-manager.ts _persist).
			return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o644)
		},
	}
	r.buffer(map[string]any{
		"type": "session", "version": CurrentSessionVersion, "id": id, "timestamp": ts, "cwd": resolved,
	})
	if model != nil {
		r.appendEntry(map[string]any{
			"type": "model_change", "provider": model.Provider, "modelId": model.ID,
		})
	}
	if len(thinkingLevel) > 0 && thinkingLevel[0] != "" {
		r.appendEntry(map[string]any{
			"type": "thinking_level_change", "thinkingLevel": thinkingLevel[0],
		})
	}
	return r, nil
}

// ResumeSession opens an existing session file for appending, porting pi's
// SessionManager.setSessionFile resume semantics (session-manager.ts:898-954):
// entries load from the file and are migrated to the current version (a v1/v2
// file is rewritten in place, as pi's _loadEntries does), the leaf is the
// file's last entry (new entries branch from it), and the manager is marked
// flushed so every subsequent entry appends to the file immediately (no
// withhold-until-assistant buffering).
func ResumeSession(path string) (*SessionRecorder, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, migrated := readSessionEntries(data)
	var id, lastID string
	byID := map[string]bool{}
	for _, e := range entries {
		entryID, _ := e["id"].(string)
		if e["type"] == "session" {
			if id == "" { // pi: entries.find(e => e.type === "session")
				id = entryID
			}
			continue
		}
		if entryID != "" {
			byID[entryID] = true
			lastID = entryID
		}
	}
	if id == "" {
		return nil, fmt.Errorf("not a pi session file (no session header): %s", path)
	}
	// Both writes below happen only AFTER the session header validates — pi
	// never writes to a file that is not a pi session.
	if migrated {
		if err := rewriteSessionFile(path, entries); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	// Repair an unterminated tail (pi 0b5ee5d8b, issue #8345): a file whose last
	// line has no trailing newline would have the next appended entry fused onto
	// it, losing both. pi repairs inside its shared loader, but only after the
	// header check; the port keeps that ordering. The read-only loaders
	// (readSessionInfo, LoadSessionMessages, LoadSessionTree) split on "\n" and
	// already tolerate an unterminated tail, so appending is the only path where
	// the corruption can occur. A rewritten file is newline-terminated already.
	if !migrated && len(data) > 0 && data[len(data)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &SessionRecorder{
		path:    path,
		id:      id,
		lastID:  lastID,
		file:    f,
		byID:    byID,
		flushed: true,
		hasAsst: true, // resumed sessions append immediately (pi flushed=true)
	}, nil
}

// Path returns the session file path.
func (r *SessionRecorder) Path() string { return r.path }

// ID returns the session id.
func (r *SessionRecorder) ID() string { return r.id }

func writeLine(f *os.File, entry map[string]any) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// buffer records an entry and persists it per pi's _persist policy: writes are
// withheld until the buffered entries contain an assistant message; once that
// happens the whole buffer is flushed atomically and later entries append.
func (r *SessionRecorder) buffer(entry map[string]any) {
	r.pending = append(r.pending, entry)
	if t, _ := entry["type"].(string); t == "message" {
		if isAssistantEntry(entry) {
			r.hasAsst = true
		}
	}
	r.persist()
}

func (r *SessionRecorder) persist() {
	if !r.hasAsst {
		// No assistant message yet: nothing reaches disk (the file is not even
		// created). Subsequent flush will write every pending entry.
		r.flushed = false
		return
	}
	if !r.flushed {
		f, err := r.createFn()
		if err != nil {
			return
		}
		r.file = f
		for _, e := range r.pending {
			writeLine(f, e)
		}
		r.flushed = true
		return
	}
	// Already flushed: append just the most recent entry.
	if r.file != nil && len(r.pending) > 0 {
		writeLine(r.file, r.pending[len(r.pending)-1])
	}
}

func isAssistantEntry(entry map[string]any) bool {
	raw, ok := entry["message"].(json.RawMessage)
	if !ok {
		return false
	}
	var head struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return false
	}
	return head.Role == "assistant"
}

func (r *SessionRecorder) appendEntry(entry map[string]any) string {
	id := genEntryID(func(id string) bool { return r.byID[id] })
	r.byID[id] = true
	entry["id"] = id
	if r.lastID == "" {
		entry["parentId"] = nil
	} else {
		entry["parentId"] = r.lastID
	}
	entry["timestamp"] = isoNow()
	r.lastID = id
	r.buffer(entry)
	return id
}

// LastEntryID returns the id of the most recently written entry (a branch point).
func (r *SessionRecorder) LastEntryID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastID
}

// ForkFrom sets the parent for subsequent entries to entryID, so new entries
// branch off an earlier point in the tree instead of extending the latest leaf.
func (r *SessionRecorder) ForkFrom(entryID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastID = entryID
}

// RecordMessage appends a message entry for an agent transcript message and
// returns its entry id (usable as a fork point).
func (r *SessionRecorder) RecordMessage(m agent.AgentMessage) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return r.appendEntry(map[string]any{"type": "message", "message": json.RawMessage(raw)})
}

// RecordThinkingLevel appends a thinking-level-change entry.
func (r *SessionRecorder) RecordThinkingLevel(level string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendEntry(map[string]any{"type": "thinking_level_change", "thinkingLevel": level})
}

// RecordModelChange appends a model-change entry.
func (r *SessionRecorder) RecordModelChange(provider, modelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendEntry(map[string]any{"type": "model_change", "provider": provider, "modelId": modelID})
}

// Close closes the session file.
func (r *SessionRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// ListSessions returns stored sessions for cwd, newest first.
func ListSessions(cwd string) []SessionInfo {
	dir := DefaultSessionDir(cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var infos []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, ok := readSessionInfo(filepath.Join(dir, e.Name())); ok {
			infos = append(infos, info)
		}
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Timestamp > infos[j].Timestamp })
	return infos
}

func readSessionInfo(path string) (SessionInfo, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionInfo{}, false
	}
	info := SessionInfo{Path: path}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var head struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Cwd       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal([]byte(line), &head) != nil {
			continue
		}
		switch head.Type {
		case "session":
			info.ID = head.ID
			info.Cwd = head.Cwd
			info.Timestamp = head.Timestamp
		case "message":
			info.Messages++
		}
	}
	return info, info.ID != ""
}

// LoadSessionMessages reconstructs the LLM message transcript from a session
// file for resume. It routes through SessionTree.BuildContext so compacted,
// branched, and custom-message sessions resume identically to pi (emitting the
// compaction summary in place of the pre-compaction turns) rather than naively
// concatenating every message entry.
func LoadSessionMessages(path string) ([]agent.AgentMessage, error) {
	tree, err := LoadSessionTree(path)
	if err != nil {
		return nil, err
	}
	return tree.BuildContext().Messages, nil
}

// LatestSession returns the most recent stored session for cwd, if any.
func LatestSession(cwd string) (SessionInfo, bool) {
	infos := ListSessions(cwd)
	if len(infos) == 0 {
		return SessionInfo{}, false
	}
	return infos[0], true
}
