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
		"claude-haiku-4-5",
		"claude-haiku-4-20250506",
		"gpt-4",
		"gpt-4-turbo",
		"codex-mini",
	} {
		if _, ok := PricingTable[model]; ok {
			t.Errorf("PricingTable contains removed compatibility alias %q", model)
		}
	}
}

func TestPricingTable_MatchesVerifiedCanonicalPrices(t *testing.T) {
	want := map[string]ModelPricing{
		"claude-opus-4-7":            {Input: 5.0, Output: 25.0},
		"claude-sonnet-4-6":          {Input: 3.0, Output: 15.0},
		"claude-haiku-4-5-20251001":  {Input: 1.0, Output: 5.0},
		"claude-opus-4-20250514":     {Input: 15.0, Output: 75.0},
		"claude-sonnet-4-20250514":   {Input: 3.0, Output: 15.0},
		"claude-3-5-sonnet-20241022": {Input: 3.0, Output: 15.0},
		"claude-3-5-haiku-20241022":  {Input: 0.80, Output: 4.0},
		"claude-3-opus-20240229":     {Input: 15.0, Output: 75.0},
		"claude-3-sonnet-20240229":   {Input: 3.0, Output: 15.0},
		"claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25},
		"gpt-5.5":                    {Input: 5.0, Output: 30.0},
		"gpt-5.5-pro":                {Input: 30.0, Output: 180.0},
		"gpt-5.4":                    {Input: 2.5, Output: 15.0},
		"gpt-5.4-mini":               {Input: 0.75, Output: 4.5},
		"gpt-5.4-nano":               {Input: 0.20, Output: 1.25},
		"gpt-5.4-pro":                {Input: 30.0, Output: 180.0},
		"gpt-5.2":                    {Input: 1.75, Output: 14.0},
		"gpt-5.2-pro":                {Input: 21.0, Output: 168.0},
		"gpt-5.1":                    {Input: 1.25, Output: 10.0},
		"gpt-5":                      {Input: 1.25, Output: 10.0},
		"gpt-5-mini":                 {Input: 0.25, Output: 2.0},
		"gpt-5-nano":                 {Input: 0.05, Output: 0.4},
		"gpt-5-pro":                  {Input: 15.0, Output: 120.0},
		"gpt-4.1":                    {Input: 2.0, Output: 8.0},
		"gpt-4.1-mini":               {Input: 0.4, Output: 1.6},
		"gpt-4.1-nano":               {Input: 0.1, Output: 0.4},
		"gpt-4o":                     {Input: 2.5, Output: 10.0},
		"gpt-4o-2024-05-13":          {Input: 5.0, Output: 15.0},
		"gpt-4o-mini":                {Input: 0.15, Output: 0.6},
		"o1":                         {Input: 15.0, Output: 60.0},
		"o1-pro":                     {Input: 150.0, Output: 600.0},
		"o1-mini":                    {Input: 1.1, Output: 4.4},
		"o3-pro":                     {Input: 20.0, Output: 80.0},
		"o3":                         {Input: 2.0, Output: 8.0},
		"o3-mini":                    {Input: 1.1, Output: 4.4},
		"o4-mini":                    {Input: 1.1, Output: 4.4},
		"gpt-4-turbo-2024-04-09":     {Input: 10.0, Output: 30.0},
		"gpt-4-0613":                 {Input: 30.0, Output: 60.0},
		"gpt-4-0314":                 {Input: 30.0, Output: 60.0},
		"gpt-4-32k":                  {Input: 60.0, Output: 120.0},
	}

	if len(PricingTable) != len(want) {
		t.Fatalf("PricingTable has %d entries, want %d", len(PricingTable), len(want))
	}
	for model, wantPricing := range want {
		got, ok := PricingTable[model]
		if !ok {
			t.Errorf("PricingTable missing verified model %q", model)
			continue
		}
		if !floatEquals(got.Input, wantPricing.Input) || !floatEquals(got.Output, wantPricing.Output) {
			t.Errorf("PricingTable[%q] = %+v, want %+v", model, got, wantPricing)
		}
	}
	for model := range PricingTable {
		if _, ok := want[model]; !ok {
			t.Errorf("PricingTable contains unverified model %q", model)
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
