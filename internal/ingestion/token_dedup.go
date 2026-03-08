package ingestion

// DeduplicateTokens removes duplicate token counts from events that share
// the same MessageUUID. Claude Code writes one JSONL line per content block,
// so a single API call producing [thinking, text] generates two lines with
// identical token values. Without deduplication, SUM() aggregation counts
// tokens N times (N = number of content blocks per API call).
//
// For each group of events sharing a MessageUUID, only the LAST event keeps
// its token values — the last content block has the most accurate output_tokens
// (thinking blocks get artificially low values during streaming). All earlier
// events in the group have their token fields zeroed out.
//
// Events with an empty MessageUUID are left unchanged.
func DeduplicateTokens(events []NormalizedEvent) []NormalizedEvent {
	if len(events) <= 1 {
		return events
	}

	// Find the last index for each MessageUUID
	lastIndex := make(map[string]int)
	for i, evt := range events {
		if evt.MessageUUID != "" {
			lastIndex[evt.MessageUUID] = i
		}
	}

	// Zero out tokens on non-last events within each UUID group
	for i := range events {
		if events[i].MessageUUID == "" {
			continue
		}
		if i != lastIndex[events[i].MessageUUID] {
			events[i].InputTokens = 0
			events[i].OutputTokens = 0
			events[i].CacheReadTokens = 0
			events[i].CacheCreateTokens = 0
		}
	}

	return events
}
