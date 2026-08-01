package protocol

// SessionSummary is the cheap view of a session, carried in server snapshots.
type SessionSummary struct {
	ID            string        `cbor:"id"`
	Name          *string       `cbor:"name,omitempty"`
	Cwd           string        `cbor:"cwd"`
	CreatedAt     int64         `cbor:"createdAt"`
	UpdatedAt     int64         `cbor:"updatedAt"`
	Phase         SessionPhase  `cbor:"phase"`
	Model         ModelRef      `cbor:"model"`
	ThinkingLevel ThinkingLevel `cbor:"thinkingLevel"`
	Attached      bool          `cbor:"attached"`
	Locked        bool          `cbor:"locked"`
}

func (s *SessionSummary) Validate() error {
	if err := requireID("id", s.ID); err != nil {
		return err
	}
	if err := requireNonEmpty("cwd", s.Cwd); err != nil {
		return err
	}
	if err := requireTimestamp("createdAt", s.CreatedAt); err != nil {
		return err
	}
	if err := requireTimestamp("updatedAt", s.UpdatedAt); err != nil {
		return err
	}
	if err := s.Phase.Validate(); err != nil {
		return err
	}
	if err := s.Model.Validate(); err != nil {
		return err
	}
	return s.ThinkingLevel.Validate()
}

// SessionSnapshot is the authoritative view of one session.
type SessionSnapshot struct {
	ID               string               `cbor:"id"`
	Name             *string              `cbor:"name,omitempty"`
	Cwd              string               `cbor:"cwd"`
	CreatedAt        int64                `cbor:"createdAt"`
	UpdatedAt        int64                `cbor:"updatedAt"`
	Phase            SessionPhase         `cbor:"phase"`
	Model            ModelRef             `cbor:"model"`
	ThinkingLevel    ThinkingLevel        `cbor:"thinkingLevel"`
	Attached         bool                 `cbor:"attached"`
	Locked           bool                 `cbor:"locked"`
	Revision         int64                `cbor:"revision"`
	Transcript       []TranscriptItem     `cbor:"transcript"`
	QueuedSteer      []UserTranscriptItem `cbor:"queuedSteer"`
	QueuedSteerCount int64                `cbor:"queuedSteerCount"`
}

func (s *SessionSnapshot) Validate() error {
	summary := SessionSummary{
		ID: s.ID, Name: s.Name, Cwd: s.Cwd, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Phase: s.Phase, Model: s.Model, ThinkingLevel: s.ThinkingLevel,
		Attached: s.Attached, Locked: s.Locked,
	}
	if err := summary.Validate(); err != nil {
		return err
	}
	if err := requireNonNegative("revision", s.Revision); err != nil {
		return err
	}
	for _, item := range s.Transcript {
		if item == nil {
			return invalidf("transcript must not contain empty items")
		}
		if err := validateItem(item); err != nil {
			return err
		}
	}
	for i := range s.QueuedSteer {
		if err := s.QueuedSteer[i].Validate(); err != nil {
			return err
		}
	}
	return requireNonNegative("queuedSteerCount", s.QueuedSteerCount)
}

// Summary projects the cheap view out of a snapshot.
func (s *SessionSnapshot) Summary() SessionSummary {
	return SessionSummary{
		ID: s.ID, Name: s.Name, Cwd: s.Cwd, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Phase: s.Phase, Model: s.Model, ThinkingLevel: s.ThinkingLevel,
		Attached: s.Attached, Locked: s.Locked,
	}
}

// ServerSnapshot is the authoritative view of the whole server.
type ServerSnapshot struct {
	ServerID        string           `cbor:"serverId"`
	ProtocolVersion int64            `cbor:"protocolVersion"`
	Revision        int64            `cbor:"revision"`
	Sessions        []SessionSummary `cbor:"sessions"`
	Models          []ModelMetadata  `cbor:"models"`
}

func (s *ServerSnapshot) Validate() error {
	if err := requireID("serverId", s.ServerID); err != nil {
		return err
	}
	if s.ProtocolVersion != ProtocolVersion {
		return invalidf("protocolVersion must be %d, got %d", ProtocolVersion, s.ProtocolVersion)
	}
	if err := requireNonNegative("revision", s.Revision); err != nil {
		return err
	}
	for i := range s.Sessions {
		if err := s.Sessions[i].Validate(); err != nil {
			return err
		}
	}
	for i := range s.Models {
		if err := s.Models[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Command is a request a client can issue.
type Command interface{ CommandName() string }

// ListCommand asks for every session.
type ListCommand struct {
	Command string `cbor:"command"`
}

func (ListCommand) CommandName() string { return "list" }
func (c *ListCommand) Validate() error  { return requireLiteral("command", c.Command, "list") }

// NewListCommand builds a well-formed list command.
func NewListCommand() *ListCommand { return &ListCommand{Command: "list"} }

// CreateCommand starts a new session.
type CreateCommand struct {
	Command       string         `cbor:"command"`
	Cwd           *string        `cbor:"cwd,omitempty"`
	Name          *string        `cbor:"name,omitempty"`
	Model         *ModelRef      `cbor:"model,omitempty"`
	ThinkingLevel *ThinkingLevel `cbor:"thinkingLevel,omitempty"`
}

func (CreateCommand) CommandName() string { return "create" }

func (c *CreateCommand) Validate() error {
	if err := requireLiteral("command", c.Command, "create"); err != nil {
		return err
	}
	if c.Cwd != nil {
		if err := requireNonEmpty("cwd", *c.Cwd); err != nil {
			return err
		}
	}
	if c.Model != nil {
		if err := c.Model.Validate(); err != nil {
			return err
		}
	}
	if c.ThinkingLevel != nil {
		return c.ThinkingLevel.Validate()
	}
	return nil
}

// sessionScopedCommand is the shape shared by every command that names a
// session and carries nothing else.
type sessionScopedCommand struct {
	Command   string `cbor:"command"`
	SessionID string `cbor:"sessionId"`
}

func (c *sessionScopedCommand) validate(want string) error {
	if err := requireLiteral("command", c.Command, want); err != nil {
		return err
	}
	return requireID("sessionId", c.SessionID)
}

// AttachCommand subscribes to a session's events.
type AttachCommand sessionScopedCommand

func (AttachCommand) CommandName() string { return "attach" }
func (c *AttachCommand) Validate() error  { return (*sessionScopedCommand)(c).validate("attach") }

// NewAttachCommand builds a well-formed attach command.
func NewAttachCommand(sessionID string) *AttachCommand {
	return &AttachCommand{Command: "attach", SessionID: sessionID}
}

// DetachCommand unsubscribes from a session.
type DetachCommand sessionScopedCommand

func (DetachCommand) CommandName() string { return "detach" }
func (c *DetachCommand) Validate() error  { return (*sessionScopedCommand)(c).validate("detach") }

// NewDetachCommand builds a well-formed detach command.
func NewDetachCommand(sessionID string) *DetachCommand {
	return &DetachCommand{Command: "detach", SessionID: sessionID}
}

// AbortCommand stops the active turn.
type AbortCommand sessionScopedCommand

func (AbortCommand) CommandName() string { return "abort" }
func (c *AbortCommand) Validate() error  { return (*sessionScopedCommand)(c).validate("abort") }

// NewAbortCommand builds a well-formed abort command.
func NewAbortCommand(sessionID string) *AbortCommand {
	return &AbortCommand{Command: "abort", SessionID: sessionID}
}

// promptPayload is the shape shared by prompt and steer.
type promptPayload struct {
	Command   string `cbor:"command"`
	SessionID string `cbor:"sessionId"`
	Text      string `cbor:"text"`
}

func (p *promptPayload) validate(want string) error {
	if err := requireLiteral("command", p.Command, want); err != nil {
		return err
	}
	return requireID("sessionId", p.SessionID)
}

// PromptCommand submits a new user turn.
type PromptCommand promptPayload

func (PromptCommand) CommandName() string { return "prompt" }
func (c *PromptCommand) Validate() error  { return (*promptPayload)(c).validate("prompt") }

// NewPromptCommand builds a well-formed prompt command.
func NewPromptCommand(sessionID, text string) *PromptCommand {
	return &PromptCommand{Command: "prompt", SessionID: sessionID, Text: text}
}

// SteerCommand injects text into the running turn.
type SteerCommand promptPayload

func (SteerCommand) CommandName() string { return "steer" }
func (c *SteerCommand) Validate() error  { return (*promptPayload)(c).validate("steer") }

// NewSteerCommand builds a well-formed steer command.
func NewSteerCommand(sessionID, text string) *SteerCommand {
	return &SteerCommand{Command: "steer", SessionID: sessionID, Text: text}
}

// SetModelCommand switches the session's model.
type SetModelCommand struct {
	Command   string   `cbor:"command"`
	SessionID string   `cbor:"sessionId"`
	Model     ModelRef `cbor:"model"`
}

func (SetModelCommand) CommandName() string { return "set_model" }

func (c *SetModelCommand) Validate() error {
	if err := requireLiteral("command", c.Command, "set_model"); err != nil {
		return err
	}
	if err := requireID("sessionId", c.SessionID); err != nil {
		return err
	}
	return c.Model.Validate()
}

// NewSetModelCommand builds a well-formed set_model command.
func NewSetModelCommand(sessionID string, model ModelRef) *SetModelCommand {
	return &SetModelCommand{Command: "set_model", SessionID: sessionID, Model: model}
}

// SetThinkingCommand switches the session's thinking level.
type SetThinkingCommand struct {
	Command       string        `cbor:"command"`
	SessionID     string        `cbor:"sessionId"`
	ThinkingLevel ThinkingLevel `cbor:"thinkingLevel"`
}

func (SetThinkingCommand) CommandName() string { return "set_thinking" }

func (c *SetThinkingCommand) Validate() error {
	if err := requireLiteral("command", c.Command, "set_thinking"); err != nil {
		return err
	}
	if err := requireID("sessionId", c.SessionID); err != nil {
		return err
	}
	return c.ThinkingLevel.Validate()
}

// NewSetThinkingCommand builds a well-formed set_thinking command.
func NewSetThinkingCommand(sessionID string, level ThinkingLevel) *SetThinkingCommand {
	return &SetThinkingCommand{Command: "set_thinking", SessionID: sessionID, ThinkingLevel: level}
}

// CommandResult is a successful reply to a Command.
type CommandResult interface{ ResultCommandName() string }

// ListResult answers list.
type ListResult struct {
	Command  string           `cbor:"command"`
	Sessions []SessionSummary `cbor:"sessions"`
}

func (ListResult) ResultCommandName() string { return "list" }

func (r *ListResult) Validate() error {
	if err := requireLiteral("command", r.Command, "list"); err != nil {
		return err
	}
	for i := range r.Sessions {
		if err := r.Sessions[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}

// DetachResult answers detach.
type DetachResult struct {
	Command   string `cbor:"command"`
	SessionID string `cbor:"sessionId"`
}

func (DetachResult) ResultCommandName() string { return "detach" }

func (r *DetachResult) Validate() error {
	if err := requireLiteral("command", r.Command, "detach"); err != nil {
		return err
	}
	return requireID("sessionId", r.SessionID)
}

// SessionResult answers every command that returns the session it acted on:
// create, attach, prompt, steer, abort, set_model, and set_thinking. pi
// declares one schema per command; they are structurally identical, so the port
// keeps one type carrying its own command name.
type SessionResult struct {
	Command string          `cbor:"command"`
	Session SessionSnapshot `cbor:"session"`
}

func (r SessionResult) ResultCommandName() string { return r.Command }

var sessionResultCommands = []string{
	"create", "attach", "prompt", "steer", "abort", "set_model", "set_thinking",
}

func (r *SessionResult) Validate() error {
	found := false
	for _, name := range sessionResultCommands {
		if r.Command == name {
			found = true
			break
		}
	}
	if !found {
		return invalidf("unknown session result command %q", r.Command)
	}
	return r.Session.Validate()
}

// ClientHello must be the first frame a client sends. Version is deliberately
// an integer, not a coercible string.
type ClientHello struct {
	Type    string `cbor:"type"`
	Version int64  `cbor:"version"`
	Token   string `cbor:"token"`
}

func (ClientHello) clientMessage() {}

func (m *ClientHello) Validate() error {
	if err := requireLiteral("type", m.Type, "hello"); err != nil {
		return err
	}
	if err := requireNonNegative("version", m.Version); err != nil {
		return err
	}
	return requireNonEmpty("token", m.Token)
}

// NewClientHello builds a hello for the version this package speaks.
func NewClientHello(token string) *ClientHello {
	return &ClientHello{Type: "hello", Version: ProtocolVersion, Token: token}
}

// RequestEnvelope carries one command and the id its response will echo.
type RequestEnvelope struct {
	Type    string  `cbor:"type"`
	ID      string  `cbor:"id"`
	Request Command `cbor:"request"`
}

func (RequestEnvelope) clientMessage() {}

func (m *RequestEnvelope) Validate() error {
	if err := requireLiteral("type", m.Type, "request"); err != nil {
		return err
	}
	if err := requireID("id", m.ID); err != nil {
		return err
	}
	if m.Request == nil {
		return invalidf("request is required")
	}
	if v, ok := m.Request.(Validator); ok {
		return v.Validate()
	}
	return nil
}

// NewRequest wraps a command in an envelope.
func NewRequest(id string, command Command) *RequestEnvelope {
	return &RequestEnvelope{Type: "request", ID: id, Request: command}
}

// ClientMessage is anything a client may send: ClientHello or RequestEnvelope.
type ClientMessage interface{ clientMessage() }

// ServerEvent is an unsolicited push from the server.
type ServerEvent interface{ EventType() string }

// ServerSnapshotEvent republishes the whole-server view.
type ServerSnapshotEvent struct {
	Type     string         `cbor:"type"`
	Snapshot ServerSnapshot `cbor:"snapshot"`
}

func (ServerSnapshotEvent) EventType() string { return "server_snapshot" }

func (e *ServerSnapshotEvent) Validate() error {
	if err := requireLiteral("type", e.Type, "server_snapshot"); err != nil {
		return err
	}
	return e.Snapshot.Validate()
}

// SessionSnapshotEvent republishes one session.
type SessionSnapshotEvent struct {
	Type     string          `cbor:"type"`
	Snapshot SessionSnapshot `cbor:"snapshot"`
}

func (SessionSnapshotEvent) EventType() string { return "session_snapshot" }

func (e *SessionSnapshotEvent) Validate() error {
	if err := requireLiteral("type", e.Type, "session_snapshot"); err != nil {
		return err
	}
	return e.Snapshot.Validate()
}

// SessionProgressEvent carries incremental transcript activity.
type SessionProgressEvent struct {
	Type      string             `cbor:"type"`
	SessionID string             `cbor:"sessionId"`
	Progress  TranscriptProgress `cbor:"progress"`
}

func (SessionProgressEvent) EventType() string { return "session_progress" }

func (e *SessionProgressEvent) Validate() error {
	if err := requireLiteral("type", e.Type, "session_progress"); err != nil {
		return err
	}
	if err := requireID("sessionId", e.SessionID); err != nil {
		return err
	}
	if e.Progress == nil {
		return invalidf("progress is required")
	}
	if v, ok := e.Progress.(Validator); ok {
		return v.Validate()
	}
	return nil
}

// SessionRemovedEvent announces a session is gone.
type SessionRemovedEvent struct {
	Type      string `cbor:"type"`
	SessionID string `cbor:"sessionId"`
}

func (SessionRemovedEvent) EventType() string { return "session_removed" }

func (e *SessionRemovedEvent) Validate() error {
	if err := requireLiteral("type", e.Type, "session_removed"); err != nil {
		return err
	}
	return requireID("sessionId", e.SessionID)
}

// ServerHello answers a ClientHello.
type ServerHello struct {
	Type         string         `cbor:"type"`
	Version      int64          `cbor:"version"`
	ConnectionID string         `cbor:"connectionId"`
	Snapshot     ServerSnapshot `cbor:"snapshot"`
}

func (ServerHello) serverMessage() {}

func (m *ServerHello) Validate() error {
	if err := requireLiteral("type", m.Type, "hello"); err != nil {
		return err
	}
	if m.Version != ProtocolVersion {
		return invalidf("version must be %d, got %d", ProtocolVersion, m.Version)
	}
	if err := requireID("connectionId", m.ConnectionID); err != nil {
		return err
	}
	return m.Snapshot.Validate()
}

// ServerHelloError rejects a handshake.
type ServerHelloError struct {
	Type  string        `cbor:"type"`
	Error ProtocolError `cbor:"error"`
}

func (ServerHelloError) serverMessage() {}

func (m *ServerHelloError) Validate() error {
	if err := requireLiteral("type", m.Type, "hello_error"); err != nil {
		return err
	}
	return m.Error.Validate()
}

// ResponseEnvelope answers a RequestEnvelope.
//
// pi splits this into an ok:true arm carrying result and an ok:false arm
// carrying error. One struct with both optional reproduces both arms — only one
// is ever populated, so the field order on the wire is identical either way.
type ResponseEnvelope struct {
	Type   string         `cbor:"type"`
	ID     string         `cbor:"id"`
	OK     bool           `cbor:"ok"`
	Result CommandResult  `cbor:"result,omitempty"`
	Error  *ProtocolError `cbor:"error,omitempty"`
}

func (ResponseEnvelope) serverMessage() {}

func (m *ResponseEnvelope) Validate() error {
	if err := requireLiteral("type", m.Type, "response"); err != nil {
		return err
	}
	if err := requireID("id", m.ID); err != nil {
		return err
	}
	if m.OK {
		if m.Result == nil {
			return invalidf("a successful response requires a result")
		}
		if m.Error != nil {
			return invalidf("a successful response must not carry an error")
		}
		if v, ok := m.Result.(Validator); ok {
			return v.Validate()
		}
		return nil
	}
	if m.Error == nil {
		return invalidf("a failed response requires an error")
	}
	if m.Result != nil {
		return invalidf("a failed response must not carry a result")
	}
	return m.Error.Validate()
}

// EventEnvelope wraps a ServerEvent.
type EventEnvelope struct {
	Type  string      `cbor:"type"`
	Event ServerEvent `cbor:"event"`
}

func (EventEnvelope) serverMessage() {}

func (m *EventEnvelope) Validate() error {
	if err := requireLiteral("type", m.Type, "event"); err != nil {
		return err
	}
	if m.Event == nil {
		return invalidf("event is required")
	}
	if v, ok := m.Event.(Validator); ok {
		return v.Validate()
	}
	return nil
}

// ServerMessage is anything a server may send.
type ServerMessage interface{ serverMessage() }
