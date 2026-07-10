package ai

import (
	"math"
	"testing"
)

// Mirrors pi packages/ai/test/models-runtime.test.ts (upstream a9ecf301): a
// request-wide pricing tier applies only when total input usage strictly
// exceeds the tier threshold.
func TestCalculateCostAppliesInputTiers(t *testing.T) {
	model := &Model{Cost: ModelCost{
		Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 6.25,
		Tiers: []ModelCostTier{{
			InputTokensAbove: 272_000,
			Input:            10,
			Output:           45,
			CacheRead:        1,
			CacheWrite:       12.5,
		}},
	}}
	usage := func(cacheWrite int) *Usage {
		return &Usage{Input: 200_000, Output: 100_000, CacheRead: 72_000, CacheWrite: cacheWrite}
	}
	approx := func(name string, got, want float64) {
		if math.Abs(got-want) > 1e-12 {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}

	// inputTokens = 200000 + 72000 + 0 = 272000, NOT > 272000 -> base rates.
	short := CalculateCost(model, usage(0))
	approx("short.Input", short.Input, 1)
	approx("short.Output", short.Output, 3)
	approx("short.CacheRead", short.CacheRead, 0.036)
	approx("short.CacheWrite", short.CacheWrite, 0)

	// inputTokens = 272001 > 272000 -> tier rates.
	long := CalculateCost(model, usage(1))
	approx("long.Input", long.Input, 2)
	approx("long.Output", long.Output, 4.5)
	approx("long.CacheRead", long.CacheRead, 0.072)
	approx("long.CacheWrite", long.CacheWrite, 0.0000125)
}
