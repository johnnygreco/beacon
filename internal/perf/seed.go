package perf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

// SeedSize controls the dataset size.
type SeedSize string

const (
	SizeSmall  SeedSize = "small"
	SizeMedium SeedSize = "medium"
	SizeLarge  SeedSize = "large"
	SizeFleet  SeedSize = "fleet"
)

// ParseSeedSize converts a string to SeedSize, defaulting to SizeSmall.
func ParseSeedSize(s string) SeedSize {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "medium":
		return SizeMedium
	case "large":
		return SizeLarge
	case "fleet":
		return SizeFleet
	default:
		return SizeSmall
	}
}

// Profile describes the intended shape of a synthetic performance dataset.
type Profile struct {
	Size                 SeedSize
	Description          string
	Heavy                bool
	Nodes                int
	Collectors           int
	Sources              int
	Runtimes             int
	Projects             int
	Sessions             int
	ActiveSessions       int
	IdleSessions         int
	TargetEvents         int
	TargetPayloads       int
	TargetSearchPostings int
	CommonSearchToken    string
	ScopedCollectorID    string
	ScopedSourceID       string
	ScopedProjectKey     string
}

// Stats holds seeding statistics.
type Stats struct {
	Sessions       int
	Events         int
	Payloads       int
	SearchPostings int
	Nodes          int
	Collectors     int
	Sources        int
	Runtimes       int
	Projects       int
	ActiveSessions int
	IdleSessions   int
	Duration       time.Duration
}

func (s Stats) String() string {
	return fmt.Sprintf("sessions=%d events=%d payloads=%d search_postings=%d collectors=%d runtimes=%d active=%d idle=%d duration=%s",
		s.Sessions, s.Events, s.Payloads, s.SearchPostings, s.Collectors, s.Runtimes, s.ActiveSessions, s.IdleSessions, s.Duration.Truncate(time.Millisecond))
}

type seedConfig struct {
	sessions       int
	activeSessions int
	idleSessions   int
	normalMin      int
	normalMax      int
	largeCount     int
	largeMin       int
	largeMax       int
	veryLargeCount int
	veryLargeMin   int
	veryLargeMax   int
	subagentFrac   float64
	nodeCount      int
	collectorCount int
	projectCount   int
	targetPayloads int
	targetPostings int
	payloadEvery   int
	heavy          bool
}

func configFor(size SeedSize) seedConfig {
	switch size {
	case SizeMedium:
		return seedConfig{
			sessions: 2500, activeSessions: 250, idleSessions: 500, normalMin: 40, normalMax: 120,
			largeCount: 50, largeMin: 500, largeMax: 1200,
			veryLargeCount: 10, veryLargeMin: 2000, veryLargeMax: 3000,
			subagentFrac: 0.1, nodeCount: 25, collectorCount: 25, projectCount: 50,
		}
	case SizeLarge:
		return seedConfig{
			sessions: 10000, activeSessions: 750, idleSessions: 1500, normalMin: 40, normalMax: 120,
			largeCount: 50, largeMin: 500, largeMax: 1500,
			veryLargeCount: 20, veryLargeMin: 2000, veryLargeMax: 4000,
			subagentFrac: 0.1, nodeCount: 25, collectorCount: 25, projectCount: 100,
		}
	case SizeFleet:
		return seedConfig{
			sessions: 100000, activeSessions: 2500, idleSessions: 2500, normalMin: 110, normalMax: 160,
			largeCount: 500, largeMin: 1000, largeMax: 2000,
			veryLargeCount: 100, veryLargeMin: 5000, veryLargeMax: 7000,
			subagentFrac: 0.1, nodeCount: 25, collectorCount: 25, projectCount: 250,
			targetPayloads: 1000000, targetPostings: 100000000, payloadEvery: 9, heavy: true,
		}
	default: // SizeSmall
		return seedConfig{
			sessions: 250, activeSessions: 25, idleSessions: 50, normalMin: 40, normalMax: 120,
			largeCount: 5, largeMin: 500, largeMax: 800,
			subagentFrac: 0.1, nodeCount: 25, collectorCount: 25, projectCount: 25,
		}
	}
}

const commonSearchToken = "fleetcommon"

// ProfileFor returns the declared fleet shape for a seed size.
func ProfileFor(size SeedSize) Profile {
	cfg := configFor(size)
	profile := Profile{
		Size:                 size,
		Description:          "generic multi-machine, multi-runtime fleet profile",
		Heavy:                cfg.heavy,
		Nodes:                cfg.nodeCount,
		Collectors:           cfg.collectorCount,
		Sources:              cfg.collectorCount * len(seedRuntimeProfiles),
		Runtimes:             len(seedRuntimeProfiles),
		Projects:             cfg.projectCount,
		Sessions:             cfg.sessions,
		ActiveSessions:       cfg.activeSessions,
		IdleSessions:         cfg.idleSessions,
		TargetEvents:         estimateTargetEvents(cfg),
		TargetPayloads:       estimateTargetPayloads(cfg),
		TargetSearchPostings: estimateTargetSearchPostings(cfg),
		CommonSearchToken:    commonSearchToken,
		ScopedCollectorID:    collectorIDForSeed(0),
		ScopedSourceID:       sourceIDForSeed(0, seedRuntimeProfiles[0]),
		ScopedProjectKey:     projectKeyForSeed(0),
	}
	return profile
}

func estimateTargetEvents(cfg seedConfig) int {
	normal := cfg.sessions - cfg.largeCount - cfg.veryLargeCount
	if normal < 0 {
		normal = 0
	}
	return normal*((cfg.normalMin+cfg.normalMax)/2) +
		cfg.largeCount*((cfg.largeMin+cfg.largeMax)/2) +
		cfg.veryLargeCount*((cfg.veryLargeMin+cfg.veryLargeMax)/2)
}

func estimateTargetPayloads(cfg seedConfig) int {
	if cfg.targetPayloads > 0 {
		return cfg.targetPayloads
	}
	return estimateTargetEvents(cfg) * 2 / 3
}

func estimateTargetSearchPostings(cfg seedConfig) int {
	if cfg.targetPostings > 0 {
		return cfg.targetPostings
	}
	return estimateTargetEvents(cfg) * 18
}

// seedBatchSize is the number of sessions per ClickHouse flush.
const seedBatchSize = 50

// Seed populates the database with deterministic test data.
func Seed(ctx context.Context, ch *store.Store, size SeedSize) (Stats, error) {
	start := time.Now()
	cfg := configFor(size)
	rng := rand.New(rand.NewSource(42))

	var stats Stats
	profile := ProfileFor(size)
	// Pre-compute session event counts
	sessionEvents := make([]int, cfg.sessions)
	for i := range sessionEvents {
		if i < cfg.veryLargeCount {
			sessionEvents[i] = cfg.veryLargeMin + rng.Intn(max(1, cfg.veryLargeMax-cfg.veryLargeMin))
		} else if i < cfg.veryLargeCount+cfg.largeCount {
			sessionEvents[i] = cfg.largeMin + rng.Intn(max(1, cfg.largeMax-cfg.largeMin))
		} else {
			sessionEvents[i] = cfg.normalMin + rng.Intn(max(1, cfg.normalMax-cfg.normalMin))
		}
	}

	baseTime := time.Now().Add(-7 * 24 * time.Hour)

	for batchStart := 0; batchStart < cfg.sessions; batchStart += seedBatchSize {
		batchEnd := batchStart + seedBatchSize
		if batchEnd > cfg.sessions {
			batchEnd = cfg.sessions
		}

		if err := seedBatch(ctx, ch, rng, cfg, sessionEvents, baseTime, batchStart, batchEnd, &stats); err != nil {
			return stats, err
		}
	}

	if err := refreshSeedStats(ctx, ch, cfg, &stats); err != nil {
		return stats, err
	}
	if err := validateSeedStats(stats, profile); err != nil {
		return stats, err
	}
	stats.Duration = time.Since(start)
	return stats, nil
}

func seedBatch(ctx context.Context, ch *store.Store, rng *rand.Rand, cfg seedConfig, sessionEvents []int, baseTime time.Time, batchStart, batchEnd int, stats *Stats) error {
	var batch store.RowBatch
	for s := batchStart; s < batchEnd; s++ {
		sessionID := fmt.Sprintf("perf-sess-%05d", s)
		parentSessID := ""
		if s > cfg.sessions/5 && rng.Float64() < cfg.subagentFrac {
			parentSessID = fmt.Sprintf("perf-sess-%05d", rng.Intn(s))
		}

		source := seedSourceForSession(s, cfg)
		runtimeProfile := source.profile
		nodeID := source.nodeID
		collectorID := collectorIDForSeed(source.collectorIndex)
		sourceID := source.sourceID
		batchID := fmt.Sprintf("batch-perf-%05d", s)
		rawSessionID := fmt.Sprintf("%s-native-%05d", runtimeProfile.SourceName, s)
		sourceFile := fmt.Sprintf("/var/beacon/perf/%s/%s/session-%05d.%s", nodeID, runtimeProfile.Runtime, s, runtimeProfile.Extension)

		numEvents := sessionEvents[s]
		timing := seedTimingForSession(s, cfg, numEvents, baseTime, rng, time.Now())
		eventTime := timing.eventTime(0)
		model := pickModel(rng, runtimeProfile.Provider)
		cwd := fmt.Sprintf("/home/user/projects/%s", projectKeyForSeed(s%cfg.projectCount))
		eventCtx := seedEventContext{
			SessionID:         sessionID,
			RawSessionID:      rawSessionID,
			ParentSessionID:   parentSessID,
			NodeID:            nodeID,
			CollectorID:       collectorID,
			SourceID:          sourceID,
			SourceName:        runtimeProfile.SourceName,
			Runtime:           runtimeProfile.Runtime,
			Provider:          runtimeProfile.Provider,
			Format:            runtimeProfile.Format,
			BatchID:           batchID,
			ControlPlaneEpoch: "1",
			CWD:               cwd,
			SourceFile:        sourceFile,
			SessionIndex:      s,
		}

		eventIdx := 0

		// session_meta event
		uid := seedUID(s, eventIdx)
		appendSeedEvent(&batch, stats, eventCtx, uid, models.EventKindSessionMeta, "init", "", eventTime, commonText(s, eventIdx), "", "", model, 0, 0, 0, 0, 0, "", "", eventIdx)
		eventIdx++

		// Generate conversation turns until we reach numEvents
		for eventIdx < numEvents {
			eventTime = timing.eventTime(eventIdx)
			turnModel := model
			if rng.Float64() < 0.1 {
				turnModel = pickModel(rng, runtimeProfile.Provider)
			}

			// User message
			uid = seedUID(s, eventIdx)
			userText := userTexts[rng.Intn(len(userTexts))] + commonText(s, eventIdx)
			appendSeedEvent(&batch, stats, eventCtx, uid, models.EventKindMessage, "text", models.ActorRoleUser, eventTime, userText, "", "", "", 0, 0, 0, 0, 0, "", "", eventIdx)
			eventIdx++
			if eventIdx >= numEvents {
				break
			}

			// Assistant message
			eventTime = timing.eventTime(eventIdx)
			uid = seedUID(s, eventIdx)
			asstText := assistantTexts[rng.Intn(len(assistantTexts))]
			if rng.Float64() < 0.15 {
				asstText += "\n\n" + largeCodeBlock
			}
			asstText += commonText(s, eventIdx)
			inTok := int64(rng.Intn(50000) + 1000)
			outTok := int64(rng.Intn(4000) + 100)
			cacheRead := int64(rng.Intn(30000))
			cacheCreate := int64(rng.Intn(5000))
			appendSeedEvent(&batch, stats, eventCtx, uid, models.EventKindMessage, "text", models.ActorRoleAssistant, eventTime, asstText, "", "", turnModel, inTok, outTok, cacheRead, cacheCreate, int64(rng.Intn(5000)+100), "", "", eventIdx)
			eventIdx++
			if eventIdx >= numEvents {
				break
			}

			// Tool calls (0-4 per turn)
			numTools := rng.Intn(5)
			for t := 0; t < numTools && eventIdx+1 < numEvents; t++ {
				toolName := toolNames[rng.Intn(len(toolNames))]
				toolUseID := fmt.Sprintf("toolu_%d_%d_%d", s, eventIdx, t)

				// tool_call event
				eventTime = timing.eventTime(eventIdx)
				uid = seedUID(s, eventIdx)
				inputJSON := withCommonJSONField(toolInputs[rng.Intn(len(toolInputs))], s, eventIdx)
				inputPreview := truncateSeed(inputJSON, 320)
				appendSeedEvent(&batch, stats, eventCtx, uid, models.EventKindToolCall, "tool_use", "", eventTime, commonText(s, eventIdx), toolName, toolUseID, turnModel, 0, 0, 0, 0, int64(rng.Intn(2000)), "", "", eventIdx)
				if shouldSeedToolPayload(cfg, s, eventIdx) {
					batch.ToolPayloads = append(batch.ToolPayloads, seedToolPayload(eventCtx, uid, toolName, models.ToolPhaseCall, inputJSON, "", inputPreview, ""))
					stats.Payloads++
				}
				eventIdx++

				// tool_result event
				eventTime = timing.eventTime(eventIdx)
				resultUID := seedUID(s, eventIdx)
				outputText := toolOutputs[rng.Intn(len(toolOutputs))] + commonText(s, eventIdx)
				outputPreview := truncateSeed(outputText, 320)
				appendSeedEvent(&batch, stats, eventCtx, resultUID, models.EventKindToolResult, models.EventKindToolResult, "", eventTime, outputText, toolName, toolUseID, "", 0, 0, 0, 0, 0, "", "", eventIdx)
				if shouldSeedToolPayload(cfg, s, eventIdx) {
					batch.ToolPayloads = append(batch.ToolPayloads, seedToolPayload(eventCtx, resultUID, toolName, models.ToolPhaseResult, "", outputText, "", outputPreview))
					stats.Payloads++
				}
				eventIdx++
			}

			// Occasional error event (5% chance)
			if eventIdx < numEvents && rng.Float64() < 0.05 {
				eventTime = timing.eventTime(eventIdx)
				uid = seedUID(s, eventIdx)
				errMsg := errorMessages[rng.Intn(len(errorMessages))] + commonText(s, eventIdx)
				appendSeedEvent(&batch, stats, eventCtx, uid, models.EventKindError, models.EventKindError, models.ActorRoleSystem, eventTime, errMsg, "", "", "", 0, 0, 0, 0, 0, "rate_limit", errMsg, eventIdx)
				eventIdx++
			}
		}

		stats.Sessions++
	}

	return ch.Flush(ctx, batch)
}

type seedSourceAssignment struct {
	nodeID         string
	collectorIndex int
	sourceID       string
	profile        seedRuntimeProfile
}

func seedSourceForSession(sessionIndex int, cfg seedConfig) seedSourceAssignment {
	sourceCount := max(1, cfg.collectorCount*len(seedRuntimeProfiles))
	sourceSlot := sessionIndex % sourceCount
	collectorIndex := sourceSlot % cfg.collectorCount
	runtimeIndex := (sourceSlot / cfg.collectorCount) % len(seedRuntimeProfiles)
	profile := seedRuntimeProfiles[runtimeIndex]
	return seedSourceAssignment{
		nodeID:         nodeIDForSeed(collectorIndex % cfg.nodeCount),
		collectorIndex: collectorIndex,
		sourceID:       sourceIDForSeed(collectorIndex, profile),
		profile:        profile,
	}
}

type seedTiming struct {
	start time.Time
	step  time.Duration
}

func (t seedTiming) eventTime(index int) time.Time {
	if index <= 0 {
		return t.start
	}
	return t.start.Add(time.Duration(index) * t.step)
}

func seedTimingForSession(sessionIndex int, cfg seedConfig, events int, baseTime time.Time, rng *rand.Rand, now time.Time) seedTiming {
	step := time.Duration(800+rng.Intn(600)) * time.Millisecond
	span := time.Duration(max(0, events-1)) * step

	var end time.Time
	switch {
	case isActiveSeedSession(sessionIndex, cfg):
		// Give active fixtures a small clock-skew cushion so a long fleet seed does
		// not age the earliest active rows out of the dashboard's five-minute window.
		end = now.Add(time.Duration(1+rng.Intn(3)) * time.Minute)
	case isIdleSeedSession(sessionIndex, cfg):
		end = now.Add(-time.Duration(10+rng.Intn(12*60)) * time.Minute)
	default:
		end = baseTime.Add(time.Duration(rng.Int63n(int64(6 * 24 * time.Hour))))
	}
	return seedTiming{start: end.Add(-span), step: step}
}

func isActiveSeedSession(sessionIndex int, cfg seedConfig) bool {
	return sessionIndex >= activeSeedStart(cfg)
}

func isIdleSeedSession(sessionIndex int, cfg seedConfig) bool {
	return sessionIndex >= idleSeedStart(cfg) && sessionIndex < activeSeedStart(cfg)
}

func activeSeedStart(cfg seedConfig) int {
	return max(0, cfg.sessions-cfg.activeSessions)
}

func idleSeedStart(cfg seedConfig) int {
	return max(0, cfg.sessions-cfg.activeSessions-cfg.idleSessions)
}

func shouldSeedToolPayload(cfg seedConfig, sessionIndex, eventIndex int) bool {
	if cfg.payloadEvery <= 1 {
		return true
	}
	return (sessionIndex*131+eventIndex)%cfg.payloadEvery == 0
}

func refreshSeedStats(ctx context.Context, ch *store.Store, cfg seedConfig, stats *Stats) error {
	var sessions, events, nodes, collectors, sources, runtimes, projects uint64
	if err := ch.DB.QueryRowContext(ctx, `SELECT
			uniqExact(session_id),
			count(),
			uniqExact(node_id),
			uniqExact(collector_id),
			uniqExact(source_id),
			uniqExact(runtime),
			uniqExact(cwd)
		FROM activity_events`).Scan(&sessions, &events, &nodes, &collectors, &sources, &runtimes, &projects); err != nil {
		return fmt.Errorf("count seeded activity shape: %w", err)
	}
	stats.Sessions = int(sessions)
	stats.Events = int(events)
	stats.Nodes = int(nodes)
	stats.Collectors = int(collectors)
	stats.Sources = int(sources)
	stats.Runtimes = int(runtimes)
	stats.Projects = int(projects)

	var payloads uint64
	if err := ch.DB.QueryRowContext(ctx, `SELECT count() FROM tool_payloads`).Scan(&payloads); err != nil {
		return fmt.Errorf("count seeded tool payloads: %w", err)
	}
	stats.Payloads = int(payloads)

	var postings uint64
	if err := ch.DB.QueryRowContext(ctx, `SELECT count() FROM search_postings`).Scan(&postings); err != nil {
		return fmt.Errorf("count seeded search postings: %w", err)
	}
	stats.SearchPostings = int(postings)

	activeStart := sessionIDForIndex(activeSeedStart(cfg))
	idleStart := sessionIDForIndex(idleSeedStart(cfg))
	cutoff := time.Now().Add(-5 * time.Minute)
	var active, idle uint64
	if err := ch.DB.QueryRowContext(ctx, `SELECT
			countIf(session_id >= ? AND ended_at >= ? AND COALESCE(has_session_end, 0) = 0),
			countIf(session_id >= ? AND session_id < ? AND ended_at < ? AND COALESCE(has_session_end, 0) = 0)
		FROM session_projection FINAL`,
		activeStart, cutoff, idleStart, activeStart, cutoff).Scan(&active, &idle); err != nil {
		return fmt.Errorf("count seeded active/idle sessions: %w", err)
	}
	stats.ActiveSessions = int(active)
	stats.IdleSessions = int(idle)
	return nil
}

func validateSeedStats(stats Stats, profile Profile) error {
	exact := []struct {
		name string
		got  int
		want int
	}{
		{"sessions", stats.Sessions, profile.Sessions},
		{"nodes", stats.Nodes, profile.Nodes},
		{"collectors", stats.Collectors, profile.Collectors},
		{"sources", stats.Sources, profile.Sources},
		{"runtimes", stats.Runtimes, profile.Runtimes},
		{"projects", stats.Projects, profile.Projects},
		{"active_sessions", stats.ActiveSessions, profile.ActiveSessions},
		{"idle_sessions", stats.IdleSessions, profile.IdleSessions},
	}
	for _, check := range exact {
		if check.got != check.want {
			return fmt.Errorf("seeded %s = %d, want %d", check.name, check.got, check.want)
		}
	}
	if profile.Heavy && profile.TargetPayloads > 0 {
		low := profile.TargetPayloads * 3 / 4
		high := profile.TargetPayloads * 5 / 4
		if stats.Payloads < low || stats.Payloads > high {
			return fmt.Errorf("seeded payloads = %d, want within %d..%d for %s profile", stats.Payloads, low, high, profile.Size)
		}
	}
	if profile.TargetSearchPostings > 0 {
		if profile.Heavy {
			if stats.SearchPostings < profile.TargetSearchPostings {
				return fmt.Errorf("seeded search postings = %d, want at least %d for %s profile", stats.SearchPostings, profile.TargetSearchPostings, profile.Size)
			}
		} else {
			low := profile.TargetSearchPostings * 3 / 4
			high := profile.TargetSearchPostings * 5 / 4
			if stats.SearchPostings < low || stats.SearchPostings > high {
				return fmt.Errorf("seeded search postings = %d, want within %d..%d for %s profile", stats.SearchPostings, low, high, profile.Size)
			}
		}
	}
	return nil
}

type seedRuntimeProfile struct {
	SourceName string
	Runtime    string
	Provider   string
	Format     string
	Extension  string
}

type seedEventContext struct {
	SessionID         string
	RawSessionID      string
	ParentSessionID   string
	NodeID            string
	CollectorID       string
	SourceID          string
	SourceName        string
	Runtime           string
	Provider          string
	Format            string
	BatchID           string
	ControlPlaneEpoch string
	CWD               string
	SourceFile        string
	SessionIndex      int
}

var seedRuntimeProfiles = []seedRuntimeProfile{
	{SourceName: "claude", Runtime: models.RuntimeClaudeCode, Provider: models.ProviderAnthropic, Format: models.FormatJSONL, Extension: "jsonl"},
	{SourceName: "codex", Runtime: models.RuntimeCodex, Provider: models.ProviderOpenAI, Format: models.FormatJSONL, Extension: "jsonl"},
	{SourceName: "hermes", Runtime: models.RuntimeHermesAgent, Provider: models.ProviderMulti, Format: models.FormatSQLite, Extension: "sqlite"},
	{SourceName: "opencode", Runtime: models.RuntimeOpenCode, Provider: models.ProviderMulti, Format: models.FormatSQLite, Extension: "sqlite"},
	{SourceName: "pi", Runtime: models.RuntimePiCodingAgent, Provider: models.ProviderMulti, Format: models.FormatJSONL, Extension: "jsonl"},
}

func appendSeedEvent(batch *store.RowBatch, stats *Stats, eventCtx seedEventContext, uid, kind, payloadType, role string, ts time.Time, text, toolName, toolUseID, model string, inputTokens, outputTokens, cacheRead, cacheCreate, durationMs int64, errorCode, errorMessage string, eventIndex int) {
	payloadDigest := seedDigest(uid)
	event := models.Event{
		EventUID:          uid,
		SessionID:         eventCtx.SessionID,
		RawSessionID:      eventCtx.RawSessionID,
		ParentSessionID:   eventCtx.ParentSessionID,
		NodeID:            eventCtx.NodeID,
		CollectorID:       eventCtx.CollectorID,
		SourceID:          eventCtx.SourceID,
		SourceName:        eventCtx.SourceName,
		Runtime:           eventCtx.Runtime,
		Provider:          eventCtx.Provider,
		Format:            eventCtx.Format,
		EventKind:         kind,
		PayloadType:       payloadType,
		ActorRole:         role,
		Timestamp:         ts,
		TextContent:       text,
		TextPreview:       truncateSeed(text, 320),
		ToolName:          toolName,
		ToolUseID:         toolUseID,
		Model:             model,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		CacheReadTokens:   cacheRead,
		CacheCreateTokens: cacheCreate,
		DurationMs:        durationMs,
		ErrorCode:         errorCode,
		ErrorMessage:      errorMessage,
		EventVersion:      1,
		PayloadJSON:       fmt.Sprintf(`{"event_uid":%q,"session_id":%q,"common_token":%q}`, uid, eventCtx.RawSessionID, commonSearchToken),
		CWD:               eventCtx.CWD,
		SourceFile:        eventCtx.SourceFile,
		SourceLineNo:      eventCtx.SessionIndex,
		SourceOffset:      int64(eventIndex),
		RawEventID:        fmt.Sprintf("%s-native-event-%05d", eventCtx.RawSessionID, eventIndex),
		SourceEventIndex:  uint64(eventIndex),
		BatchID:           eventCtx.BatchID,
		ControlPlaneEpoch: eventCtx.ControlPlaneEpoch,
		PayloadDigest:     payloadDigest,
		RedactionStatus:   "redacted",
		RedactionVersion:  "redact-v1",
	}
	batch.ActivityEvents = append(batch.ActivityEvents, event)
	batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	stats.Events++
}

func seedToolPayload(eventCtx seedEventContext, eventUID, toolName, phase, inputJSON, outputJSON, inputPreview, outputPreview string) models.ToolPayload {
	return models.ToolPayload{
		EventUID:          eventUID,
		CollectorID:       eventCtx.CollectorID,
		SourceID:          eventCtx.SourceID,
		ToolName:          toolName,
		ToolPhase:         phase,
		InputJSON:         inputJSON,
		OutputJSON:        outputJSON,
		InputPreview:      inputPreview,
		OutputPreview:     outputPreview,
		BatchID:           eventCtx.BatchID,
		ControlPlaneEpoch: eventCtx.ControlPlaneEpoch,
		PayloadDigest:     seedDigest(eventUID),
		RedactionStatus:   "redacted",
		RedactionVersion:  "redact-v1",
	}
}

func seedUID(session, event int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("seed|%d|%d", session, event)))
	return hex.EncodeToString(h[:16])
}

func seedDigest(value string) string {
	h := sha256.Sum256([]byte("payload|" + value))
	return "sha256:" + hex.EncodeToString(h[:])
}

func truncateSeed(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func pickModel(rng *rand.Rand, provider string) string {
	if provider == models.ProviderOpenAI {
		m := openaiModels[rng.Intn(len(openaiModels))]
		return m
	}
	if provider == models.ProviderMulti {
		return multiModels[rng.Intn(len(multiModels))]
	}
	return anthropicModels[rng.Intn(len(anthropicModels))]
}

func nodeIDForSeed(index int) string {
	return fmt.Sprintf("node-perf-%02d", index)
}

func collectorIDForSeed(index int) string {
	return fmt.Sprintf("collector-perf-%02d", index)
}

func sourceIDForSeed(collectorIndex int, profile seedRuntimeProfile) string {
	return fmt.Sprintf("source-perf-%02d-%s", collectorIndex, profile.Runtime)
}

func projectKeyForSeed(index int) string {
	return fmt.Sprintf("project-%03d", index)
}

func commonText(sessionIndex, eventIndex int) string {
	if eventIndex%4 != 0 {
		return ""
	}
	return fmt.Sprintf(" %s node_%02d project_%03d", commonSearchToken, sessionIndex%25, sessionIndex%250)
}

func withCommonJSONField(input string, sessionIndex, eventIndex int) string {
	if eventIndex%4 != 0 || !strings.HasSuffix(input, "}") {
		return input
	}
	return strings.TrimSuffix(input, "}") + fmt.Sprintf(`,"common_token":"%s","project_hint":"%s"}`, commonSearchToken, projectKeyForSeed(sessionIndex%250))
}

// SessionIDForBench returns a deterministic session ID for benchmark queries.
// idx=0 gives a very-large session, idx in [1..5] gives large sessions.
func SessionIDForBench(idx int) string {
	return sessionIDForIndex(idx)
}

// EventUIDForBench returns the deterministic event UID for a seeded event.
func EventUIDForBench(sessionIndex, eventIndex int) string {
	return seedUID(sessionIndex, eventIndex)
}

func sessionIDForIndex(index int) string {
	return fmt.Sprintf("perf-sess-%05d", index)
}

// --- Content arrays for deterministic data generation ---

var anthropicModels = []string{
	"claude-sonnet-4-6",
	"claude-opus-4-7",
	"claude-haiku-4-5-20251001",
}

var openaiModels = []string{
	"gpt-5.4",
	"gpt-5.4-mini",
	"o4-mini",
}

var multiModels = []string{
	"multi-router-large",
	"local-coding-agent",
	"cloud-coding-agent",
}

var toolNames = []string{
	"Read", "Write", "Edit", "Bash", "Glob", "Grep",
	"Agent", "WebSearch", "mcp__github__search", "mcp__slack__send",
}

var userTexts = []string{
	"How do I implement binary search in Go?",
	"Debug this SQL query that returns duplicate rows",
	"Refactor the auth middleware to use JWT tokens",
	"Write tests for the payment processing module",
	"Explain cache invalidation in this codebase",
	"Optimize this slow database query with proper indexing",
	"Set up error handling for the REST API endpoints",
	"Implement connection pooling for the database layer",
	"Fix the race condition in concurrent map access",
	"Add structured logging to the background worker",
	"Create a REST endpoint for user profile management",
	"Help me understand this complex regex pattern",
	"Convert this callback code to use Go channels",
	"Add rate limiting to the API gateway middleware",
	"Set up CI/CD pipeline with GitHub Actions",
}

var assistantTexts = []string{
	"I'll analyze the code and help you with that implementation.",
	"The issue is in the WHERE clause. Here's the corrected query.",
	"Here's a recursive implementation of the binary search tree.",
	"Let me examine the middleware chain to understand the flow.",
	"I've added comprehensive tests covering edge cases and errors.",
	"The query can be optimized by adding a composite index on (session_id, timestamp).",
	"I'll set up proper error types and error-handling middleware.",
	"Using sync.Pool for connection reuse is the recommended approach here.",
	"The race detector found the issue — the map needs a sync.RWMutex.",
	"Structured logging with slog provides the best observability.",
}

var largeCodeBlock = "Here is the complete implementation:\n\n```go\npackage main\n\nimport (\n\t\"context\"\n\t\"database/sql\"\n\t\"fmt\"\n\t\"log\"\n\t\"net/http\"\n\t\"sync\"\n\t\"time\"\n)\n\ntype Server struct {\n\tdb     *sql.DB\n\tcache  sync.Map\n\tlogger *log.Logger\n}\n\nfunc (s *Server) HandleRequest(w http.ResponseWriter, r *http.Request) {\n\tctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)\n\tdefer cancel()\n\n\tkey := r.URL.Query().Get(\"key\")\n\tif cached, ok := s.cache.Load(key); ok {\n\t\tfmt.Fprintf(w, \"%v\", cached)\n\t\treturn\n\t}\n\n\tvar result string\n\terr := s.db.QueryRowContext(ctx, \"SELECT value FROM cache WHERE key = ?\", key).Scan(&result)\n\tif err != nil {\n\t\thttp.Error(w, \"not found\", 404)\n\t\treturn\n\t}\n\n\ts.cache.Store(key, result)\n\tfmt.Fprint(w, result)\n}\n\nfunc (s *Server) ListenAndServe(addr string) error {\n\tmux := http.NewServeMux()\n\tmux.HandleFunc(\"/get\", s.HandleRequest)\n\treturn http.ListenAndServe(addr, mux)\n}\n```"

var toolInputs = []string{
	`{"file_path":"/src/main.go"}`,
	`{"command":"go test ./...","description":"Run all tests"}`,
	`{"pattern":"*.go","path":"internal/"}`,
	`{"pattern":"func.*Search","type":"go"}`,
	`{"file_path":"/src/config.go","old_string":"timeout: 30","new_string":"timeout: 60"}`,
	`{"prompt":"Find all database query functions","description":"Search codebase"}`,
	`{"command":"git status","description":"Check working tree status"}`,
	`{"file_path":"/src/handler.go"}`,
	`{"pattern":"TODO|FIXME|HACK"}`,
	`{"command":"curl -s http://localhost:8080/health"}`,
}

var toolOutputs = []string{
	"package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
	"PASS\nok  \tgithub.com/example/project\t0.042s",
	"internal/web/handlers.go\ninternal/web/queries.go\ninternal/search/search.go",
	"Found 3 matches in 2 files",
	"File edited successfully",
	"On branch main\nnothing to commit, working tree clean",
	"HTTP/1.1 200 OK\n{\"status\":\"healthy\"}",
	"func SearchEvents(ctx context.Context, q string) ([]Result, error) {\n\treturn nil, nil\n}",
	"No matches found",
	"Build succeeded with 0 errors and 2 warnings",
}

var errorMessages = []string{
	"Rate limit exceeded. Please retry after 30 seconds.",
	"API request timed out after 60 seconds",
	"Connection refused: database server is not running",
	"Permission denied: insufficient access rights",
	"Internal server error: unexpected nil pointer",
}

// ResetAndSeed clears the database and seeds it with test data.
func ResetAndSeed(ctx context.Context, ch *store.Store, size SeedSize) (Stats, error) {
	if err := store.Reset(ctx, ch.DB, ch.Database()); err != nil {
		return Stats{}, fmt.Errorf("reset schema: %w", err)
	}
	return Seed(ctx, ch, size)
}

// SeedEvent creates a single event for testing. Exported for use in test helpers.
func SeedEvent(uid, sessionID, kind, role, text, model string, inTok, outTok int64) *models.Event {
	now := time.Now()
	return &models.Event{
		EventUID:     uid,
		SessionID:    sessionID,
		EventKind:    kind,
		ActorRole:    role,
		TextContent:  text,
		TextPreview:  truncateSeed(text, 320),
		Model:        model,
		InputTokens:  inTok,
		OutputTokens: outTok,
		Timestamp:    now,
		EventVersion: 1,
	}
}
