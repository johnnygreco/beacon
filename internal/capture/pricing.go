package capture

// ModelPricing holds per-1M-token costs.
type ModelPricing struct {
	Input  float64 // cost per 1M input tokens
	Output float64 // cost per 1M output tokens
}

// PricingLastVerified records when PricingTable was checked against provider docs.
const PricingLastVerified = "2026-05-22"

// PricingTable maps canonical model identifiers to their standard API pricing.
// Compatibility aliases are intentionally omitted so unknown aliases use the
// configured fallback pricing instead of stale hard-coded values.
var PricingTable = map[string]ModelPricing{
	// Anthropic current canonical IDs.
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

	// OpenAI text model IDs.
	"gpt-5.5":                {Input: 5.0, Output: 30.0},
	"gpt-5.5-pro":            {Input: 30.0, Output: 180.0},
	"gpt-5.4":                {Input: 2.5, Output: 15.0},
	"gpt-5.4-mini":           {Input: 0.75, Output: 4.5},
	"gpt-5.4-nano":           {Input: 0.20, Output: 1.25},
	"gpt-5.4-pro":            {Input: 30.0, Output: 180.0},
	"gpt-5.2":                {Input: 1.75, Output: 14.0},
	"gpt-5.2-pro":            {Input: 21.0, Output: 168.0},
	"gpt-5.1":                {Input: 1.25, Output: 10.0},
	"gpt-5":                  {Input: 1.25, Output: 10.0},
	"gpt-5-mini":             {Input: 0.25, Output: 2.0},
	"gpt-5-nano":             {Input: 0.05, Output: 0.4},
	"gpt-5-pro":              {Input: 15.0, Output: 120.0},
	"gpt-4.1":                {Input: 2.0, Output: 8.0},
	"gpt-4.1-mini":           {Input: 0.4, Output: 1.6},
	"gpt-4.1-nano":           {Input: 0.1, Output: 0.4},
	"gpt-4o":                 {Input: 2.5, Output: 10.0},
	"gpt-4o-2024-05-13":      {Input: 5.0, Output: 15.0},
	"gpt-4o-mini":            {Input: 0.15, Output: 0.6},
	"o1":                     {Input: 15.0, Output: 60.0},
	"o1-pro":                 {Input: 150.0, Output: 600.0},
	"o1-mini":                {Input: 1.1, Output: 4.4},
	"o3-pro":                 {Input: 20.0, Output: 80.0},
	"o3":                     {Input: 2.0, Output: 8.0},
	"o3-mini":                {Input: 1.1, Output: 4.4},
	"o4-mini":                {Input: 1.1, Output: 4.4},
	"gpt-4-turbo-2024-04-09": {Input: 10.0, Output: 30.0},
	"gpt-4-0613":             {Input: 30.0, Output: 60.0},
	"gpt-4-0314":             {Input: 30.0, Output: 60.0},
	"gpt-4-32k":              {Input: 60.0, Output: 120.0},
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
