package api

import "unicode/utf8"

const (
	ModelHaiku  = "claude-haiku-4-5-20251001"
	ModelSonnet = "claude-sonnet-4-6"
	ModelOpus   = "claude-opus-4-6"

	// EscalateAfterChars is the message length threshold (in Unicode code points)
	// above which selectModel auto-escalates to Sonnet 4.6.
	EscalateAfterChars = 500
)

// modelPrices maps model IDs to USD cost per 1M tokens.
// Source: Anthropic June 2026 billing documentation.
// Update when Anthropic changes rates.
var modelPrices = map[string]struct{ InputPer1M, OutputPer1M float64 }{
	ModelHaiku:  {0.80, 4.00},
	ModelSonnet: {3.00, 15.00},
	ModelOpus:   {15.00, 75.00},
}

// selectModel picks the model for a managed session turn.
// Priority: explicit user override > image presence > message length > default (Haiku).
// Message length is measured in Unicode code points, not bytes.
func selectModel(message string, hasImages bool, userOverride string) string {
	if userOverride != "" {
		return userOverride
	}
	if hasImages || utf8.RuneCountInString(message) > EscalateAfterChars {
		return ModelSonnet
	}
	return ModelHaiku
}

// calcCost returns the estimated USD cost for a single turn.
// Returns 0 for unknown models — never panics.
func calcCost(model string, inputTokens, outputTokens int) float64 {
	p, ok := modelPrices[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1e6)*p.InputPer1M +
		(float64(outputTokens)/1e6)*p.OutputPer1M
}
