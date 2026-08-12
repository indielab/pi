package coding

import (
	"os"

	"github.com/sky-valley/pi/ai"
)

// preferStrictToolSampling is pi's PREFER_STRICT_TOOL_SAMPLING (upstream
// 7915cdac6): the shared json_schema/prefer constrained-sampling config the
// experimental gate hands to the built-in tools. Like pi's module-level const,
// every gated tool shares the one value.
var preferStrictToolSampling = &ai.ConstrainedSamplingConfig{
	Type:   ai.ConstrainedSamplingJSONSchema,
	Strict: ai.ConstrainedSamplingPrefer,
}

// AreExperimentalFeaturesEnabled ports pi's areExperimentalFeaturesEnabled
// (core/experimental.ts, upstream 66335d3a): the guard that lets users opt in
// to early features. It is true only when the PI_EXPERIMENTAL environment
// variable is exactly "1" — unset, empty, "0", "true", or any other value all
// leave experimental features disabled.
func AreExperimentalFeaturesEnabled() bool {
	return os.Getenv("PI_EXPERIMENTAL") == "1"
}

// GetExperimentalToolSampling ports pi's getExperimentalToolSampling: the
// constrained-sampling config for the built-in read/bash/edit/write tools when
// experimental features are enabled, nil otherwise.
func GetExperimentalToolSampling() *ai.ConstrainedSamplingConfig {
	if AreExperimentalFeaturesEnabled() {
		return preferStrictToolSampling
	}
	return nil
}
