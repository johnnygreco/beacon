package capture

import (
	"math"
	"testing"
)

const epsilon = 0.0001

func floatEquals(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestCalcCost(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		inputTokens   int64
		outputTokens  int64
		defaultInput  float64
		defaultOutput float64
		want          float64
	}{
		{
			name:          "current claude sonnet 1M input zero output",
			model:         "claude-sonnet-4-6",
			inputTokens:   1_000_000,
			outputTokens:  0,
			defaultInput:  0,
			defaultOutput: 0,
			want:          3.00,
		},
		{
			name:          "current claude sonnet zero input 1M output",
			model:         "claude-sonnet-4-6",
			inputTokens:   0,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          15.00,
		},
		{
			name:          "current claude sonnet small token counts",
			model:         "claude-sonnet-4-6",
			inputTokens:   1000,
			outputTokens:  500,
			defaultInput:  0,
			defaultOutput: 0,
			want:          (1000.0 * 3.0 / 1_000_000) + (500.0 * 15.0 / 1_000_000),
		},
		{
			name:          "current gpt-5.4 pricing",
			model:         "gpt-5.4",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          2.50 + 15.0,
		},
		{
			name:          "unknown model uses defaults",
			model:         "unknown-model",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  5.0,
			defaultOutput: 20.0,
			want:          5.0 + 20.0,
		},
		{
			name:          "zero tokens returns zero",
			model:         "claude-sonnet-4-6",
			inputTokens:   0,
			outputTokens:  0,
			defaultInput:  0,
			defaultOutput: 0,
			want:          0.0,
		},
		{
			name:          "current claude opus pricing",
			model:         "claude-opus-4-7",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          5.0 + 25.0,
		},
		{
			name:          "removed claude alias uses defaults",
			model:         "claude-sonnet-4",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  5.0,
			defaultOutput: 20.0,
			want:          5.0 + 20.0,
		},
		{
			name:          "current claude haiku pricing",
			model:         "claude-haiku-4-5-20251001",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          1.0 + 5.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcCost(tt.model, tt.inputTokens, tt.outputTokens, tt.defaultInput, tt.defaultOutput)
			if !floatEquals(got, tt.want) {
				t.Errorf("CalcCost(%q, %d, %d, %f, %f) = %f, want %f",
					tt.model, tt.inputTokens, tt.outputTokens, tt.defaultInput, tt.defaultOutput, got, tt.want)
			}
		})
	}
}

func TestPricingTable_RemovesCompatibilityAliases(t *testing.T) {
	for _, model := range []string{
		"claude-opus-4",
		"claude-sonnet-4",
		"claude-haiku-4",
		"claude-haiku-4-20250506",
	} {
		if _, ok := PricingTable[model]; ok {
			t.Errorf("PricingTable contains removed compatibility alias %q", model)
		}
	}
}

func TestPricingTable_LastVerified(t *testing.T) {
	if PricingLastVerified != "2026-05-22" {
		t.Fatalf("PricingLastVerified = %q, want %q", PricingLastVerified, "2026-05-22")
	}
}

func TestPricingTable_AllModelsHavePositivePricing(t *testing.T) {
	for model, pricing := range PricingTable {
		if pricing.Input <= 0 {
			t.Errorf("model %q has non-positive Input pricing: %f", model, pricing.Input)
		}
		if pricing.Output <= 0 {
			t.Errorf("model %q has non-positive Output pricing: %f", model, pricing.Output)
		}
	}
}
