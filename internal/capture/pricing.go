package capture

// ModelPricing holds per-1M-token costs.
type ModelPricing struct {
	Input  float64 // cost per 1M input tokens
	Output float64 // cost per 1M output tokens
}

// PricingTable maps model identifiers to their pricing.
var PricingTable = map[string]ModelPricing{
	// Claude models
	"claude-opus-4-20250514":     {Input: 15.0, Output: 75.0},
	"claude-sonnet-4-20250514":   {Input: 3.0, Output: 15.0},
	"claude-haiku-4-20250506":    {Input: 0.80, Output: 4.0},
	"claude-3-5-sonnet-20241022": {Input: 3.0, Output: 15.0},
	"claude-3-5-haiku-20241022":  {Input: 0.80, Output: 4.0},
	"claude-3-opus-20240229":     {Input: 15.0, Output: 75.0},
	"claude-3-sonnet-20240229":   {Input: 3.0, Output: 15.0},
	"claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25},
	// Aliases
	"claude-opus-4":   {Input: 15.0, Output: 75.0},
	"claude-sonnet-4": {Input: 3.0, Output: 15.0},
	"claude-haiku-4":  {Input: 0.80, Output: 4.0},

	// OpenAI models
	"gpt-4o":      {Input: 2.50, Output: 10.0},
	"gpt-4o-mini": {Input: 0.15, Output: 0.60},
	"gpt-4-turbo": {Input: 10.0, Output: 30.0},
	"gpt-4":       {Input: 30.0, Output: 60.0},
	"o1":          {Input: 15.0, Output: 60.0},
	"o1-mini":     {Input: 3.0, Output: 12.0},
	"o3":          {Input: 10.0, Output: 40.0},
	"o3-mini":     {Input: 1.10, Output: 4.40},
	"o4-mini":     {Input: 1.10, Output: 4.40},
	"codex-mini":  {Input: 1.50, Output: 6.0},
}

// CalcCost calculates cost in USD given model name and token counts.
func CalcCost(model string, inputTokens, outputTokens int64, defaultInput, defaultOutput float64) float64 {
	pricing, ok := PricingTable[model]
	if !ok {
		return (float64(inputTokens) * defaultInput / 1_000_000) +
			(float64(outputTokens) * defaultOutput / 1_000_000)
	}
	return (float64(inputTokens) * pricing.Input / 1_000_000) +
		(float64(outputTokens) * pricing.Output / 1_000_000)
}
