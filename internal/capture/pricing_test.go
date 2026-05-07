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
			name:          "claude sonnet 1M input zero output",
			model:         "claude-sonnet-4-20250514",
			inputTokens:   1_000_000,
			outputTokens:  0,
			defaultInput:  0,
			defaultOutput: 0,
			want:          3.00,
		},
		{
			name:          "claude sonnet zero input 1M output",
			model:         "claude-sonnet-4-20250514",
			inputTokens:   0,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          15.00,
		},
		{
			name:          "claude sonnet small token counts",
			model:         "claude-sonnet-4-20250514",
			inputTokens:   1000,
			outputTokens:  500,
			defaultInput:  0,
			defaultOutput: 0,
			want:          (1000.0 * 3.0 / 1_000_000) + (500.0 * 15.0 / 1_000_000),
		},
		{
			name:          "gpt-4o pricing",
			model:         "gpt-4o",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          2.50 + 10.0,
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
			model:         "claude-sonnet-4-20250514",
			inputTokens:   0,
			outputTokens:  0,
			defaultInput:  0,
			defaultOutput: 0,
			want:          0.0,
		},
		{
			name:          "claude opus most expensive",
			model:         "claude-opus-4-20250514",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          15.0 + 75.0,
		},
		{
			name:          "alias claude-sonnet-4 matches full name pricing",
			model:         "claude-sonnet-4",
			inputTokens:   1_000_000,
			outputTokens:  1_000_000,
			defaultInput:  0,
			defaultOutput: 0,
			want:          3.0 + 15.0,
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
