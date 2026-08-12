package coding

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/sky-valley/pi/ai"
)

// Port of upstream test/experimental.test.ts (66335d3a): only the exact value
// "1" enables experimental features.
func TestAreExperimentalFeaturesEnabled(t *testing.T) {
	cases := []struct {
		name  string
		set   bool
		value string
		want  bool
	}{
		{name: "unset", set: false, want: false},
		{name: "empty", set: true, value: "", want: false},
		{name: "one", set: true, value: "1", want: true},
		{name: "zero", set: true, value: "0", want: false},
		{name: "non-1 value", set: true, value: "true", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("PI_EXPERIMENTAL", tc.value)
			} else {
				// t.Setenv registers the restore, then the variable is removed
				// to exercise the truly-unset case.
				t.Setenv("PI_EXPERIMENTAL", "")
				if err := os.Unsetenv("PI_EXPERIMENTAL"); err != nil {
					t.Fatal(err)
				}
			}
			if got := AreExperimentalFeaturesEnabled(); got != tc.want {
				t.Fatalf("AreExperimentalFeaturesEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Port of experimental-tool-strict-mode.test.ts (upstream 7915cdac6): the
// built-in read/bash/edit/write tools carry strict-prefer constrained sampling
// only under PI_EXPERIMENTAL=1, and the gate never touches their parameters.
func TestExperimentalStrictBuiltInTools(t *testing.T) {
	// t.Setenv registers the restore before the variable is removed to
	// exercise the truly-unset case.
	t.Setenv("PI_EXPERIMENTAL", "")
	if err := os.Unsetenv("PI_EXPERIMENTAL"); err != nil {
		t.Fatal(err)
	}
	normalTools := CreateCodingTools(t.TempDir())
	t.Setenv("PI_EXPERIMENTAL", "1")
	experimentalTools := CreateCodingTools(t.TempDir())

	want := &ai.ConstrainedSamplingConfig{
		Type: ai.ConstrainedSamplingJSONSchema, Strict: ai.ConstrainedSamplingPrefer,
	}
	for i, tool := range experimentalTools {
		if cs := tool.ConstrainedSampling; cs == nil || *cs != *want {
			t.Fatalf("tool %s must prefer strict sampling in experimental mode: %#v", tool.Name, cs)
		}
		gotParams, _ := json.Marshal(tool.Parameters)
		wantParams, _ := json.Marshal(normalTools[i].Parameters)
		if string(gotParams) != string(wantParams) {
			t.Fatalf("tool %s parameters must not change:\n got: %s\nwant: %s", tool.Name, gotParams, wantParams)
		}
		if normalTools[i].ConstrainedSampling != nil {
			t.Fatalf("tool %s must not constrain sampling outside experimental mode: %#v",
				normalTools[i].Name, normalTools[i].ConstrainedSampling)
		}
	}
}
