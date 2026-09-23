package delta

import (
	"encoding/json"
	"strings"
	"testing"
)

// goldenProbe is a draft array too large to record, summarized after pi's
// steps (capture.mts, "Probes: undefined slots").
type goldenProbe struct {
	Name      string  `json:"name"`
	StepError *string `json:"stepError"`
	// Returned is what the last step returned, for the probes that record it.
	Returned *struct {
		Undefined bool            `json:"undefined"`
		Value     json.RawMessage `json:"value"`
	} `json:"returned"`
	Length     int    `json:"length"`
	Keys       int    `json:"keys"`
	StringHash string `json:"stringHash"`
	Slots      []struct {
		Index     int             `json:"index"`
		Has       bool            `json:"has"`
		Undefined bool            `json:"undefined"`
		Value     json.RawMessage `json:"value"`
	} `json:"slots"`
	PrepareError *string `json:"prepareError"`
}

// probeSteps are the probes' steps as capture.mts runs them, on the draft of
// {"v": [1]}.
var probeSteps = map[string]func(v *Draft) error{
	"splice 10,001 items": func(v *Draft) error {
		return firstError(v.SetLen(3), spliceErr(v.Splice(0, 0, insertItems(10_001)...)))
	},
	"splice 10,000 items": func(v *Draft) error {
		return firstError(v.SetLen(3), spliceErr(v.Splice(0, 0, insertItems(10_000)...)))
	},
	"unshift 10,001 items": func(v *Draft) error {
		return firstError(v.SetLen(3), lengthErr(v.Unshift(insertItems(10_001)...)))
	},
	"unshift 10,000 items": func(v *Draft) error {
		return firstError(v.SetLen(3), lengthErr(v.Unshift(insertItems(10_000)...)))
	},
	"splice 10,001 items over a longer run": func(v *Draft) error {
		return firstError(v.SetLen(20_005), v.Set(20_004, 2), spliceErr(v.Splice(1, 15_000, insertItems(10_001)...)))
	},
	"splice 10,001 items replacing as many": func(v *Draft) error {
		return firstError(v.SetLen(10_005), v.Set(10_004, 2), spliceErr(v.Splice(1, 10_001, insertItems(10_001)...)))
	},
	"default sort": func(v *Draft) error {
		if err := firstError(v.SetLen(3), v.Set(2, -1), lengthErr(v.Unshift(insertItems(10_001)...))); err != nil {
			return err
		}
		return firstError(v.SetLen(v.Len()+2), v.Sort(nil))
	},
	"comparator sort": func(v *Draft) error {
		if err := firstError(v.SetLen(3), v.Set(2, -1), lengthErr(v.Unshift(insertItems(10_001)...))); err != nil {
			return err
		}
		descending := func(a, b any) int {
			switch d := b.(float64) - a.(float64); {
			case d < 0:
				return -1
			case d > 0:
				return 1
			}
			return 0
		}
		return firstError(v.SetLen(v.Len()+2), v.Sort(descending))
	},
	"shift, pop and reverse": func(v *Draft) error {
		if err := firstError(v.SetLen(3), lengthErr(v.Unshift(insertItems(10_001)...)), lengthErr(v.Push(7))); err != nil {
			return err
		}
		if err := firstError(v.SetLen(v.Len()+1), popErr(v.Shift()), v.Reverse(), popErr(v.Pop())); err != nil {
			return err
		}
		return firstError(spliceErr(v.Splice(4, 1)), lengthErr(v.Unshift(8)))
	},
	"copyWithin": func(v *Draft) error {
		return firstError(v.SetLen(3), lengthErr(v.Unshift(insertItems(10_001)...)), v.CopyWithin(0, 10_002, 10_003))
	},
	"fill and set": func(v *Draft) error {
		return firstError(v.SetLen(3), lengthErr(v.Unshift(insertItems(10_001)...)), v.Fill(5, 10_002, 10_003), v.Set(10_003, 6))
	},
}

// probeReturns are the probes whose last step returns a value, which pi
// recorded.
var probeReturns = map[string]func(v *Draft) (any, bool, error){
	"pop of an undefined slot": func(v *Draft) (any, bool, error) {
		if err := firstError(v.SetLen(3), lengthErr(v.Unshift(insertItems(10_001)...))); err != nil {
			return nil, false, err
		}
		return v.Pop()
	},
	"shift of an undefined slot": func(v *Draft) (any, bool, error) {
		if err := firstError(v.SetLen(3), lengthErr(v.Unshift(insertItems(10_001)...)), v.Reverse()); err != nil {
			return nil, false, err
		}
		return v.Shift()
	},
}

func insertItems(n int) []any {
	items := make([]any, n)
	for i := range items {
		items[i] = float64(i)
	}
	return items
}

// firstError is the first non-nil error: the step pi would have thrown at.
// Every step runs, since Go evaluates the arguments first, so a probe whose
// steps can fail puts the failing one last.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func spliceErr(_ []any, err error) error    { return err }
func lengthErr(_ int, err error) error      { return err }
func popErr(_ any, _ bool, err error) error { return err }

// Past 10,000 inserted items, pi's unshift and splice turn the holes they move
// into slots holding undefined; the probes are recorded from pi's own draft.
func TestGoldenProbes(t *testing.T) {
	g := golden(t)
	for _, want := range g.Probes {
		t.Run(want.Name, func(t *testing.T) {
			tr := mustTrack(t, `{"v": [1]}`)
			c := mustBegin(t, tr)
			v := c.State().At("v")
			var err error
			if steps, ok := probeSteps[want.Name]; ok {
				err = steps(v)
			} else if steps, ok := probeReturns[want.Name]; ok {
				var returned any
				var present bool
				returned, present, err = steps(v)
				switch {
				case want.Returned == nil:
					t.Fatalf("pi recorded no return value for %q", want.Name)
				case present == want.Returned.Undefined:
					t.Errorf("returned %v, %v; pi returned %s (undefined %v)", returned, present, want.Returned.Value, want.Returned.Undefined)
				case present && jsonText(t, returned) != string(want.Returned.Value):
					t.Errorf("returned %s, pi %s", jsonText(t, returned), want.Returned.Value)
				}
			} else {
				t.Fatalf("no steps for probe %q: add them to probeSteps or probeReturns as capture.mts runs them", want.Name)
			}
			switch {
			case want.StepError == nil && err != nil:
				t.Errorf("steps: %v, pi threw nothing", err)
			case want.StepError != nil && (err == nil || !strings.Contains(err.Error(), *want.StepError)):
				t.Errorf("steps: %v, pi threw %q", err, *want.StepError)
			}
			if got := v.Len(); got != want.Length {
				t.Errorf("length %d, pi %d", got, want.Length)
			}
			if got := len(v.Keys()); got != want.Keys {
				t.Errorf("%d keys, pi %d", got, want.Keys)
			}
			if s, err := jsString(v, nil); err != nil || sha(s) != want.StringHash {
				t.Errorf("String() differs from pi's (err %v)", err)
			}
			for _, slot := range want.Slots {
				if got := v.Has(slot.Index); got != slot.Has {
					t.Errorf("Has(%d) = %v, pi %v", slot.Index, got, slot.Has)
				}
				value, ok := v.Get(slot.Index)
				if ok == slot.Undefined {
					t.Errorf("Get(%d) = %v, %v; pi read %s (undefined %v)", slot.Index, value, ok, slot.Value, slot.Undefined)
					continue
				}
				if ok && jsonText(t, value) != string(slot.Value) {
					t.Errorf("Get(%d) = %s, pi %s", slot.Index, jsonText(t, value), slot.Value)
				}
			}
			_, err = c.Prepare()
			switch {
			case want.PrepareError == nil && err != nil:
				t.Errorf("Prepare: %v, pi prepared", err)
			case want.PrepareError != nil && (err == nil || !strings.Contains(err.Error(), *want.PrepareError)):
				t.Errorf("Prepare: %v, pi threw %q", err, *want.PrepareError)
			}
		})
	}
}
