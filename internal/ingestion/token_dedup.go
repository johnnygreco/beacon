package ingestion

// DeduplicateTokens removes duplicate token counts that arise from
// Claude Code writing multiple JSONL lines for the same API response.
//
// Claude Code writes each content block as a separate JSONL line, connected
// via parentUuid chains (e.g. thinking → text → tool_use). Each line carries
// the API's usage object, but only the LAST line in the chain has the
// cumulative output_tokens total. Earlier lines carry partial/misleading
// counts. Without dedup, SUM() aggregation over-counts both input and output.
//
// This function handles two dedup patterns:
//
//  1. Parent chains: Assistant-role lines linked via ParentUUID → MessageUUID.
//     All tokens are kept only on the last event in each chain.
//
//  2. Shared MessageUUID: Multiple events from the same JSONL line (e.g. a
//     line with [thinking, text] content). Tokens kept on the last event.
func DeduplicateTokens(events []NormalizedEvent) []NormalizedEvent {
	if len(events) <= 1 {
		return events
	}

	// --- Phase 1: Parent-chain dedup ---
	// Only consider assistant-role events for chaining (reasoning, message,
	// tool_call). User messages and tool results are separate API
	// interactions and must not be chained together.
	assistantUUIDs := make(map[string]int, len(events))
	for i, evt := range events {
		if evt.MessageUUID == "" {
			continue
		}
		if evt.ActorRole == "assistant" {
			assistantUUIDs[evt.MessageUUID] = i
		}
	}

	// Build a child map among assistant events only.
	childOf := make(map[string]int, len(events))
	for i, evt := range events {
		if evt.ParentUUID == "" || evt.ActorRole != "assistant" {
			continue
		}
		if _, parentIsAssistant := assistantUUIDs[evt.ParentUUID]; parentIsAssistant {
			childOf[evt.ParentUUID] = i
		}
	}

	// Walk chains from roots. A root is an assistant event whose parent is
	// either absent from the batch or not an assistant event.
	inChain := make(map[int]bool, len(events))
	for i, evt := range events {
		if inChain[i] || evt.ActorRole != "assistant" || evt.MessageUUID == "" {
			continue
		}
		// Skip non-roots.
		if evt.ParentUUID != "" {
			if _, parentIsAssistant := assistantUUIDs[evt.ParentUUID]; parentIsAssistant {
				continue
			}
		}
		// Must have at least one child to form a chain.
		if _, hasChild := childOf[evt.MessageUUID]; !hasChild {
			continue
		}

		// Walk forward.
		var chain []int
		chain = append(chain, i)
		inChain[i] = true
		cur := evt.MessageUUID
		for {
			childIdx, ok := childOf[cur]
			if !ok {
				break
			}
			chain = append(chain, childIdx)
			inChain[childIdx] = true
			cur = events[childIdx].MessageUUID
		}
		if len(chain) <= 1 {
			continue
		}

		// Zero tokens on all but the last event.
		for _, idx := range chain[:len(chain)-1] {
			zeroTokens(&events[idx])
		}
	}

	// --- Phase 2: Shared-UUID dedup (original behavior) ---
	lastIndex := make(map[string]int, len(events))
	for i, evt := range events {
		if evt.MessageUUID != "" {
			lastIndex[evt.MessageUUID] = i
		}
	}
	for i := range events {
		if events[i].MessageUUID == "" {
			continue
		}
		if i != lastIndex[events[i].MessageUUID] {
			zeroTokens(&events[i])
		}
	}

	return events
}

// PropagateModel forward-fills model names across events within a session.
// Some providers (e.g. Codex) only set the model on context events
// (turn_context, session_meta) while token and tool events have no model.
// This function propagates the model forward so that tokens-by-model
// queries can correctly attribute tokens to the right model.
func PropagateModel(events []NormalizedEvent) {
	// Group events by session and propagate model forward within each session.
	type sessionState struct {
		model string
	}
	sessions := make(map[string]*sessionState)

	for i := range events {
		sid := events[i].SessionID
		state, ok := sessions[sid]
		if !ok {
			state = &sessionState{}
			sessions[sid] = state
		}

		// If this event has a model, use it as the current model for the session.
		if events[i].Model != "" {
			state.model = events[i].Model
		}

		// If this event has no model but we know one, propagate it.
		if events[i].Model == "" && state.model != "" {
			events[i].Model = state.model
		}
	}
}

func zeroTokens(evt *NormalizedEvent) {
	evt.InputTokens = 0
	evt.OutputTokens = 0
	evt.CacheReadTokens = 0
	evt.CacheCreateTokens = 0
}
