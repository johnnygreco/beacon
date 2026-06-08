package capture

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

type SourceReadResult struct {
	Events        []NormalizedEvent
	CaptureErrors []models.CaptureError
	Checkpoint    *models.Checkpoint
	HasMore       bool
}

type wholeFileCheckpointState struct {
	Version       int                     `json:"version"`
	Size          int64                   `json:"size"`
	ModTimeUnixNS int64                   `json:"mod_time_unix_ns"`
	Sidecars      []wholeFileSidecarState `json:"sidecars,omitempty"`
	EventIndex    int                     `json:"event_index,omitempty"`
	Complete      bool                    `json:"complete,omitempty"`
	ErrorID       string                  `json:"error_id,omitempty"`
}

type wholeFileSidecarState struct {
	Suffix        string `json:"suffix"`
	Exists        bool   `json:"exists"`
	Size          int64  `json:"size,omitempty"`
	ModTimeUnixNS int64  `json:"mod_time_unix_ns,omitempty"`
}

func ReadSourceFile(ctx context.Context, src WatchSource, file string, cp *models.Checkpoint, logger *slog.Logger) (SourceReadResult, error) {
	return ReadSourceFileWindow(ctx, src, file, cp, logger, 0)
}

func ReadSourceFileWindow(ctx context.Context, src WatchSource, file string, cp *models.Checkpoint, logger *slog.Logger, maxRecords int) (SourceReadResult, error) {
	var result SourceReadResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	fi, err := os.Stat(file)
	if err != nil {
		return result, err
	}
	cp = checkpointAfterRotation(src, file, fi, cp)
	if src.FileParser != nil {
		return readWholeSourceFile(ctx, src, file, fi, cp, maxRecords)
	}
	return readLineSourceFile(ctx, src, file, fi, cp, logger, maxRecords)
}

func readLineSourceFile(ctx context.Context, src WatchSource, file string, fi os.FileInfo, cp *models.Checkpoint, logger *slog.Logger, maxRecords int) (SourceReadResult, error) {
	var result SourceReadResult
	var offset int64
	var lineNo int
	var checkpointOffset int64
	var initialState lineParserState
	var pending pendingLineCursor
	sourceGeneration := 0
	if cp != nil {
		offset = cp.LastOffset
		lineNo = cp.LastLineNo
		checkpointOffset = cp.LastOffset
		sourceGeneration = cp.SourceGeneration
		if fi.Size() <= offset {
			return result, nil
		}

		checkpointState, stateOK := decodeLineParserCheckpointState(cp.StateJSON, logger)
		if stateOK {
			pending = pendingLineCursorFromState(checkpointState)
		}
		if stateOK && checkpointState.ReplayStartLineNo > 0 &&
			checkpointState.ReplayStartLineNo <= cp.LastLineNo &&
			checkpointState.ReplayStartOffset <= cp.LastOffset {
			offset = checkpointState.ReplayStartOffset
			lineNo = checkpointState.ReplayStartLineNo - 1
			initialState = checkpointState.ReplayState.clone()
		} else {
			offset, lineNo = replayStartFromPrefix(file, offset, lineNo, incrementalReplayLines, logger)
		}
	}

	f, err := os.Open(file)
	if err != nil {
		return result, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			return result, err
		}
	}

	reader := bufio.NewReader(f)

	var allEvents []NormalizedEvent
	var replayLines []replayLine
	var emitIndexes []int
	var visibleRecords int

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err == io.EOF {
			break
		}
		if err != nil && err != io.EOF {
			return result, err
		}
		hasDelimiter := len(line) > 0 && line[len(line)-1] == '\n'
		if !hasDelimiter && err == io.EOF {
			break
		}
		lineStart := offset
		lineNo++
		lineLen := int64(len(line))
		lineBytes := bytes.TrimSuffix(line, []byte{'\n'})
		lineBytes = bytes.TrimSuffix(lineBytes, []byte{'\r'})
		replayLines = appendReplayLine(replayLines, replayLine{offset: offset, lineNo: lineNo}, incrementalReplayLines)

		if len(lineBytes) == 0 {
			offset += lineLen
			if err == io.EOF {
				break
			}
			continue
		}
		events, err := src.Parser(lineBytes, file, lineNo, offset)
		if err != nil {
			message := err.Error()
			fragment := truncate(string(lineBytes), 500)
			if lineStart >= checkpointOffset {
				result.CaptureErrors = append(result.CaptureErrors, models.CaptureError{
					ID:              lineParseErrorID(src.Name, file, lineNo, lineStart, sourceGeneration, message, fragment),
					SourceName:      src.Name,
					SourceFile:      file,
					SourceLineNo:    lineNo,
					SourceOffset:    offset,
					ErrorClass:      "parse_error",
					ErrorMessage:    message,
					ContextFragment: fragment,
				})
			}
			offset += lineLen
			if lineStart >= checkpointOffset {
				visibleRecords++
			}
			if sourceReadWindowFull(maxRecords, visibleRecords) {
				result.HasMore = offset < fi.Size()
				break
			}
			continue
		}

		for i := range events {
			applyWatchSourceMetadata(&events[i], src, cp)
		}
		firstEventIndex := len(allEvents)
		allEvents = append(allEvents, events...)
		emitStart := lineEmitStart(lineStart, lineNo, len(events), checkpointOffset, pending)
		emitEnd := len(events)
		if maxRecords > 0 {
			remaining := maxRecords - visibleRecords
			if remaining <= 0 {
				result.HasMore = true
				break
			}
			if emitStart+remaining < emitEnd {
				emitEnd = emitStart + remaining
			}
		}
		for eventIndex := emitStart; eventIndex < emitEnd; eventIndex++ {
			emitIndexes = append(emitIndexes, firstEventIndex+eventIndex)
		}
		visibleRecords += emitEnd - emitStart
		if emitEnd < len(events) {
			PropagateModelWithInitial(allEvents, initialState.Models)
			allEvents = DeduplicateTokensWithInitial(allEvents, initialState.TokenUsageTotals)
			nextState := buildLineParserCheckpointState(initialState, allEvents, replayLines)
			nextState.PendingLineOffset = lineStart
			nextState.PendingLineNo = lineNo
			nextState.PendingEventIndex = emitEnd
			stateJSON := encodeLineParserCheckpointState(nextState, logger)
			result.Checkpoint = &models.Checkpoint{
				SourceName:       src.Name,
				SourceFile:       file,
				SourceInode:      fileInode(fi),
				SourceGeneration: sourceGeneration,
				LastOffset:       lineStart,
				LastLineNo:       lineNo,
				StateJSON:        stateJSON,
			}
			for _, index := range emitIndexes {
				if index >= 0 && index < len(allEvents) {
					result.Events = append(result.Events, allEvents[index])
				}
			}
			result.HasMore = true
			return result, nil
		}
		offset += lineLen
		if sourceReadWindowFull(maxRecords, visibleRecords) {
			result.HasMore = offset < fi.Size()
			break
		}
		if err == io.EOF {
			break
		}
	}

	PropagateModelWithInitial(allEvents, initialState.Models)
	allEvents = DeduplicateTokensWithInitial(allEvents, initialState.TokenUsageTotals)
	nextState := buildLineParserCheckpointState(initialState, allEvents, replayLines)

	for _, index := range emitIndexes {
		if index >= 0 && index < len(allEvents) {
			result.Events = append(result.Events, allEvents[index])
		}
	}
	stateJSON := encodeLineParserCheckpointState(nextState, logger)
	if checkpointAdvanced(cp, offset, lineNo, sourceGeneration) || (len(replayLines) > 0 && (cp == nil || stateJSON != cp.StateJSON)) {
		result.Checkpoint = &models.Checkpoint{
			SourceName:       src.Name,
			SourceFile:       file,
			SourceInode:      fileInode(fi),
			SourceGeneration: sourceGeneration,
			LastOffset:       offset,
			LastLineNo:       lineNo,
			StateJSON:        stateJSON,
		}
	}
	return result, nil
}

func readWholeSourceFile(ctx context.Context, src WatchSource, file string, fi os.FileInfo, cp *models.Checkpoint, maxRecords int) (SourceReadResult, error) {
	var result SourceReadResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	beforeState := wholeFileCheckpointStateFor(src, file, fi)
	if wholeFileUnchanged(cp, beforeState) {
		return result, nil
	}
	events, err := src.FileParser(file)
	if err != nil {
		sourceGeneration := 0
		if cp != nil {
			sourceGeneration = cp.SourceGeneration
		}
		errorID := wholeFileErrorID(src.Name, file, fi, sourceGeneration, err)
		if wholeFileDuplicateParseError(cp, beforeState, errorID) {
			return result, nil
		}
		result.CaptureErrors = append(result.CaptureErrors, models.CaptureError{
			ID:              errorID,
			SourceName:      src.Name,
			SourceFile:      file,
			ErrorClass:      "parse_error",
			ErrorMessage:    err.Error(),
			ContextFragment: file,
		})
		result.Checkpoint = &models.Checkpoint{
			SourceName:       src.Name,
			SourceFile:       file,
			SourceInode:      fileInode(fi),
			SourceGeneration: sourceGeneration,
			LastOffset:       0,
			LastLineNo:       0,
			StateJSON:        encodeWholeFileCheckpointState(beforeState, 0, false, errorID),
		}
		return result, nil
	}
	for i := range events {
		applyWatchSourceMetadata(&events[i], src, cp)
	}
	PropagateModel(events)
	events = DeduplicateTokens(events)
	start := wholeFileCheckpointEventIndex(cp, beforeState)
	if start > len(events) {
		start = len(events)
	}
	end := len(events)
	complete := true
	if maxRecords > 0 && start+maxRecords < end {
		end = start + maxRecords
		complete = false
		result.HasMore = true
	}
	result.Events = events[start:end]
	checkpointState := beforeState
	if afterFI, err := os.Stat(file); err != nil {
		return result, err
	} else {
		afterState := wholeFileCheckpointStateFor(src, file, afterFI)
		if !wholeFileCheckpointStatesMatch(beforeState, afterState) {
			complete = false
			result.HasMore = true
		}
	}

	sourceGeneration := 0
	if cp != nil {
		sourceGeneration = cp.SourceGeneration
	}
	result.Checkpoint = &models.Checkpoint{
		SourceName:       src.Name,
		SourceFile:       file,
		SourceInode:      fileInode(fi),
		SourceGeneration: sourceGeneration,
		LastOffset:       wholeFileCheckpointOffset(fi, complete),
		LastLineNo:       end,
		StateJSON:        encodeWholeFileCheckpointState(checkpointState, end, complete, ""),
	}
	return result, nil
}

func wholeFileUnchanged(cp *models.Checkpoint, current wholeFileCheckpointState) bool {
	if cp == nil {
		return false
	}
	var state wholeFileCheckpointState
	if err := json.Unmarshal([]byte(cp.StateJSON), &state); err != nil {
		return false
	}
	return wholeFileCheckpointStatesMatch(state, current) && state.Complete
}

func wholeFileCheckpointEventIndex(cp *models.Checkpoint, current wholeFileCheckpointState) int {
	if cp == nil {
		return 0
	}
	var state wholeFileCheckpointState
	if err := json.Unmarshal([]byte(cp.StateJSON), &state); err != nil {
		return 0
	}
	if !wholeFileCheckpointStatesMatch(state, current) || state.Complete {
		return 0
	}
	if state.EventIndex < 0 {
		return 0
	}
	return state.EventIndex
}

func wholeFileDuplicateParseError(cp *models.Checkpoint, current wholeFileCheckpointState, errorID string) bool {
	if cp == nil || errorID == "" {
		return false
	}
	var state wholeFileCheckpointState
	if err := json.Unmarshal([]byte(cp.StateJSON), &state); err != nil {
		return false
	}
	return !state.Complete && state.ErrorID == errorID && wholeFileCheckpointStatesMatch(state, current)
}

func wholeFileCheckpointStatesMatch(left, right wholeFileCheckpointState) bool {
	if left.Version != 2 || right.Version != 2 || left.Size != right.Size || left.ModTimeUnixNS != right.ModTimeUnixNS {
		return false
	}
	if len(left.Sidecars) != len(right.Sidecars) {
		return false
	}
	for i := range left.Sidecars {
		if left.Sidecars[i] != right.Sidecars[i] {
			return false
		}
	}
	return true
}

func wholeFileCheckpointOffset(fi os.FileInfo, complete bool) int64 {
	if complete {
		return fi.Size()
	}
	return 0
}

func encodeWholeFileCheckpointState(state wholeFileCheckpointState, eventIndex int, complete bool, errorID string) string {
	state.EventIndex = eventIndex
	state.Complete = complete
	state.ErrorID = errorID
	data, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	return string(data)
}

func wholeFileCheckpointStateFor(src WatchSource, file string, fi os.FileInfo) wholeFileCheckpointState {
	return wholeFileCheckpointState{
		Version:       2,
		Size:          fi.Size(),
		ModTimeUnixNS: fi.ModTime().UnixNano(),
		Sidecars:      wholeFileSidecarStates(src, file),
	}
}

func wholeFileSidecarStates(src WatchSource, file string) []wholeFileSidecarState {
	if src.Format != models.FormatSQLite {
		return nil
	}
	states := make([]wholeFileSidecarState, 0, 2)
	for _, suffix := range []string{"-wal", "-shm"} {
		states = append(states, wholeFileSidecarStateFor(file, suffix))
	}
	return states
}

func wholeFileSidecarStateFor(file, suffix string) wholeFileSidecarState {
	state := wholeFileSidecarState{Suffix: suffix}
	fi, err := os.Stat(file + suffix)
	if err != nil {
		return state
	}
	state.Exists = true
	state.Size = fi.Size()
	state.ModTimeUnixNS = fi.ModTime().UnixNano()
	return state
}

func lineParseErrorID(sourceName, file string, lineNo int, offset int64, sourceGeneration int, message, fragment string) string {
	parts := []string{
		"line-parse-error",
		sourceName,
		file,
		strconv.Itoa(lineNo),
		strconv.FormatInt(offset, 10),
		strconv.Itoa(sourceGeneration),
		message,
		fragment,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "capture_error_" + hex.EncodeToString(sum[:])[:32]
}

func wholeFileErrorID(sourceName, file string, fi os.FileInfo, sourceGeneration int, err error) string {
	parts := []string{
		"whole-file-parse-error",
		sourceName,
		file,
		fi.ModTime().UTC().Format(time.RFC3339Nano),
		strconv.FormatInt(fi.Size(), 10),
		strconv.Itoa(sourceGeneration),
	}
	if err != nil {
		parts = append(parts, err.Error())
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "capture_error_" + hex.EncodeToString(sum[:])[:32]
}

func checkpointAdvanced(cp *models.Checkpoint, offset int64, lineNo, sourceGeneration int) bool {
	if cp == nil {
		return offset > 0 || lineNo > 0 || sourceGeneration > 0
	}
	if sourceGeneration != cp.SourceGeneration {
		return true
	}
	if offset != cp.LastOffset {
		return true
	}
	return lineNo != cp.LastLineNo
}

func sourceReadWindowFull(maxRecords, visibleRecords int) bool {
	return maxRecords > 0 && visibleRecords >= maxRecords
}

type pendingLineCursor struct {
	active     bool
	offset     int64
	lineNo     int
	eventIndex int
}

func pendingLineCursorFromState(state lineParserCheckpointState) pendingLineCursor {
	if state.PendingLineNo <= 0 || state.PendingEventIndex <= 0 {
		return pendingLineCursor{}
	}
	return pendingLineCursor{
		active:     true,
		offset:     state.PendingLineOffset,
		lineNo:     state.PendingLineNo,
		eventIndex: state.PendingEventIndex,
	}
}

func lineEmitStart(lineOffset int64, lineNo, eventCount int, checkpointOffset int64, pending pendingLineCursor) int {
	if eventCount <= 0 {
		return 0
	}
	if lineOffset < checkpointOffset {
		return eventCount
	}
	if pending.active && pending.offset == lineOffset && pending.lineNo == lineNo {
		return min(max(pending.eventIndex, 0), eventCount)
	}
	return 0
}

func checkpointAfterRotation(src WatchSource, file string, fi os.FileInfo, cp *models.Checkpoint) *models.Checkpoint {
	if cp == nil {
		return nil
	}
	rotated := false
	inode := fileInode(fi)
	if inode > 0 && cp.SourceInode > 0 && inode != cp.SourceInode {
		rotated = true
	}
	if fi.Size() < cp.LastOffset {
		rotated = true
	}
	if !rotated {
		return cp
	}
	return &models.Checkpoint{
		NodeID:           cp.NodeID,
		CollectorID:      cp.CollectorID,
		SourceID:         cp.SourceID,
		SourceName:       src.Name,
		SourceFile:       file,
		SourceInode:      inode,
		SourceGeneration: cp.SourceGeneration + 1,
	}
}
