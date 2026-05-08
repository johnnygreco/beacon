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
)

// ParseSeedSize converts a string to SeedSize, defaulting to SizeSmall.
func ParseSeedSize(s string) SeedSize {
	switch strings.ToLower(s) {
	case "medium":
		return SizeMedium
	case "large":
		return SizeLarge
	default:
		return SizeSmall
	}
}

// Stats holds seeding statistics.
type Stats struct {
	Sessions int
	Events   int
	Payloads int
	Duration time.Duration
}

func (s Stats) String() string {
	return fmt.Sprintf("sessions=%d events=%d payloads=%d duration=%s",
		s.Sessions, s.Events, s.Payloads, s.Duration.Truncate(time.Millisecond))
}

type seedConfig struct {
	sessions       int
	normalMin      int
	normalMax      int
	largeCount     int
	largeMin       int
	largeMax       int
	veryLargeCount int
	veryLargeMin   int
	veryLargeMax   int
	subagentFrac   float64
}

func configFor(size SeedSize) seedConfig {
	switch size {
	case SizeMedium:
		return seedConfig{
			sessions: 2500, normalMin: 40, normalMax: 120,
			largeCount: 50, largeMin: 500, largeMax: 1200,
			veryLargeCount: 10, veryLargeMin: 2000, veryLargeMax: 3000,
			subagentFrac: 0.1,
		}
	case SizeLarge:
		return seedConfig{
			sessions: 10000, normalMin: 40, normalMax: 120,
			largeCount: 50, largeMin: 500, largeMax: 1500,
			veryLargeCount: 20, veryLargeMin: 2000, veryLargeMax: 4000,
			subagentFrac: 0.1,
		}
	default: // SizeSmall
		return seedConfig{
			sessions: 250, normalMin: 40, normalMax: 120,
			largeCount: 5, largeMin: 500, largeMax: 800,
			subagentFrac: 0.1,
		}
	}
}

// seedBatchSize is the number of sessions per ClickHouse flush.
const seedBatchSize = 50

// Seed populates the database with deterministic test data.
func Seed(ctx context.Context, ch *store.Store, size SeedSize) (Stats, error) {
	start := time.Now()
	cfg := configFor(size)
	rng := rand.New(rand.NewSource(42))

	var stats Stats

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

		source := "claude"
		runtime := "claude-code"
		provider := "anthropic"
		if rng.Float64() < 0.15 {
			source = "codex"
			runtime = "codex"
			provider = "openai"
		}

		sessionStart := baseTime.Add(time.Duration(rng.Int63n(int64(6 * 24 * time.Hour))))
		if rng.Float64() < 0.3 {
			sessionStart = time.Now().Add(-time.Duration(rng.Int63n(int64(12 * time.Hour))))
		}

		eventTime := sessionStart
		model := pickModel(rng, provider)
		cwd := fmt.Sprintf("/home/user/projects/project-%d", s%20)
		numEvents := sessionEvents[s]

		eventIdx := 0

		// session_meta event
		uid := seedUID(s, eventIdx)
		appendSeedEvent(&batch, stats, uid, sessionID, parentSessID, source, runtime, provider, "session_meta", "init", "", eventTime, "", "", "", model, 0, 0, 0, 0, 0, "", "", cwd, s, eventIdx)
		eventIdx++

		// Generate conversation turns until we reach numEvents
		for eventIdx < numEvents {
			eventTime = eventTime.Add(time.Duration(rng.Intn(3000)+500) * time.Millisecond)
			turnModel := model
			if rng.Float64() < 0.1 {
				turnModel = pickModel(rng, provider)
			}

			// User message
			uid = seedUID(s, eventIdx)
			userText := userTexts[rng.Intn(len(userTexts))]
			appendSeedEvent(&batch, stats, uid, sessionID, parentSessID, source, runtime, provider, "message", "text", "user", eventTime, userText, "", "", "", 0, 0, 0, 0, 0, "", "", cwd, s, eventIdx)
			eventIdx++
			if eventIdx >= numEvents {
				break
			}

			// Assistant message
			eventTime = eventTime.Add(time.Duration(rng.Intn(2000)+200) * time.Millisecond)
			uid = seedUID(s, eventIdx)
			asstText := assistantTexts[rng.Intn(len(assistantTexts))]
			if rng.Float64() < 0.15 {
				asstText += "\n\n" + largeCodeBlock
			}
			inTok := int64(rng.Intn(50000) + 1000)
			outTok := int64(rng.Intn(4000) + 100)
			cacheRead := int64(rng.Intn(30000))
			cacheCreate := int64(rng.Intn(5000))
			appendSeedEvent(&batch, stats, uid, sessionID, parentSessID, source, runtime, provider, "message", "text", "assistant", eventTime, asstText, "", "", turnModel, inTok, outTok, cacheRead, cacheCreate, int64(rng.Intn(5000)+100), "", "", cwd, s, eventIdx)
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
				eventTime = eventTime.Add(time.Duration(rng.Intn(1000)+100) * time.Millisecond)
				uid = seedUID(s, eventIdx)
				inputJSON := toolInputs[rng.Intn(len(toolInputs))]
				inputPreview := truncateSeed(inputJSON, 320)
				appendSeedEvent(&batch, stats, uid, sessionID, parentSessID, source, runtime, provider, "tool_call", "tool_use", "", eventTime, "", toolName, toolUseID, turnModel, 0, 0, 0, 0, int64(rng.Intn(2000)), "", "", cwd, s, eventIdx)
				batch.ToolPayloads = append(batch.ToolPayloads, models.ToolPayload{EventUID: uid, ToolName: toolName, ToolPhase: "call", InputJSON: inputJSON, InputPreview: inputPreview})
				stats.Payloads++
				eventIdx++

				// tool_result event
				eventTime = eventTime.Add(time.Duration(rng.Intn(2000)+50) * time.Millisecond)
				resultUID := seedUID(s, eventIdx)
				outputText := toolOutputs[rng.Intn(len(toolOutputs))]
				outputPreview := truncateSeed(outputText, 320)
				appendSeedEvent(&batch, stats, resultUID, sessionID, parentSessID, source, runtime, provider, "tool_result", "tool_result", "", eventTime, outputText, toolName, toolUseID, "", 0, 0, 0, 0, 0, "", "", cwd, s, eventIdx)
				batch.ToolPayloads = append(batch.ToolPayloads, models.ToolPayload{EventUID: resultUID, ToolName: toolName, ToolPhase: "result", OutputJSON: outputText, OutputPreview: outputPreview})
				stats.Payloads++
				eventIdx++
			}

			// Occasional error event (5% chance)
			if eventIdx < numEvents && rng.Float64() < 0.05 {
				eventTime = eventTime.Add(time.Duration(rng.Intn(500)+50) * time.Millisecond)
				uid = seedUID(s, eventIdx)
				errMsg := errorMessages[rng.Intn(len(errorMessages))]
				appendSeedEvent(&batch, stats, uid, sessionID, parentSessID, source, runtime, provider, "error", "error", "system", eventTime, errMsg, "", "", "", 0, 0, 0, 0, 0, "rate_limit", errMsg, cwd, s, eventIdx)
				eventIdx++
			}
		}

		stats.Sessions++
	}

	return ch.Flush(ctx, batch)
}

func appendSeedEvent(batch *store.RowBatch, stats *Stats, uid, sessionID, parentSessionID, source, runtime, provider, kind, payloadType, role string, ts time.Time, text, toolName, toolUseID, model string, inputTokens, outputTokens, cacheRead, cacheCreate, durationMs int64, errorCode, errorMessage, cwd string, sessionIndex, eventIndex int) {
	event := models.Event{
		EventUID:          uid,
		SessionID:         sessionID,
		ParentSessionID:   parentSessionID,
		SourceName:        source,
		Runtime:           runtime,
		Provider:          provider,
		Format:            "jsonl",
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
		CWD:               cwd,
		SourceFile:        "perf-seed",
		SourceLineNo:      sessionIndex,
		SourceOffset:      int64(eventIndex),
	}
	batch.ActivityEvents = append(batch.ActivityEvents, event)
	batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	stats.Events++
}

func seedUID(session, event int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("seed|%d|%d", session, event)))
	return hex.EncodeToString(h[:16])
}

func truncateSeed(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func pickModel(rng *rand.Rand, provider string) string {
	if provider == "openai" {
		m := openaiModels[rng.Intn(len(openaiModels))]
		return m
	}
	return anthropicModels[rng.Intn(len(anthropicModels))]
}

// SessionIDForBench returns a deterministic session ID for benchmark queries.
// idx=0 gives a very-large session, idx in [1..5] gives large sessions.
func SessionIDForBench(idx int) string {
	return fmt.Sprintf("perf-sess-%05d", idx)
}

// --- Content arrays for deterministic data generation ---

var anthropicModels = []string{
	"claude-sonnet-4-20250514",
	"claude-opus-4-20250514",
	"claude-haiku-4-20250514",
}

var openaiModels = []string{
	"gpt-4o",
	"gpt-4o-mini",
	"o4-mini",
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
