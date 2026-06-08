package capture

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"

	"github.com/johnnygreco/beacon/internal/models"
)

type SourceReadResult struct {
	Events        []NormalizedEvent
	CaptureErrors []models.CaptureError
	Checkpoint    *models.Checkpoint
	HasMore       bool
}

type wholeFileCheckpointState struct {
	Version       int   `json:"version"`
	Size          int64 `json:"size"`
	ModTimeUnixNS int64 `json:"mod_time_unix_ns"`
	EventIndex    int   `json:"event_index,omitempty"`
	Complete      bool  `json:"complete,omitempty"`
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
		if len(lineBytes) > scannerMaxTokenBytes {
			lineStart := offset
			result.CaptureErrors = append(result.CaptureErrors, models.CaptureError{
				ID:              genID(),
				SourceName:      src.Name,
				SourceFile:      file,
				SourceLineNo:    lineNo,
				SourceOffset:    offset,
				ErrorClass:      "parse_error",
				ErrorMessage:    "line exceeds maximum capture token size",
				ContextFragment: truncate(string(lineBytes[:min(len(lineBytes), 500)]), 500),
			})
			offset += lineLen
			if lineStart >= checkpointOffset {
				visibleRecords++
			}
			if sourceReadWindowFull(maxRecords, visibleRecords) {
				result.HasMore = offset < fi.Size()
				break
			}
			if err == io.EOF {
				break
			}
			continue
		}

		events, err := src.Parser(lineBytes, file, lineNo, offset)
		if err != nil {
			lineStart := offset
			result.CaptureErrors = append(result.CaptureErrors, models.CaptureError{
				ID:              genID(),
				SourceName:      src.Name,
				SourceFile:      file,
				SourceLineNo:    lineNo,
				SourceOffset:    offset,
				ErrorClass:      "parse_error",
				ErrorMessage:    err.Error(),
				ContextFragment: truncate(string(lineBytes), 500),
			})
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
		allEvents = append(allEvents, events...)
		for _, event := range events {
			if event.SourceOffset >= checkpointOffset {
				visibleRecords++
			}
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

	for _, evt := range allEvents {
		if cp != nil && evt.SourceOffset < checkpointOffset {
			continue
		}
		result.Events = append(result.Events, evt)
	}
	result.Checkpoint = &models.Checkpoint{
		SourceName:       src.Name,
		SourceFile:       file,
		SourceInode:      fileInode(fi),
		SourceGeneration: sourceGeneration,
		LastOffset:       offset,
		LastLineNo:       lineNo,
		StateJSON:        encodeLineParserCheckpointState(nextState, logger),
	}
	return result, nil
}

func readWholeSourceFile(ctx context.Context, src WatchSource, file string, fi os.FileInfo, cp *models.Checkpoint, maxRecords int) (SourceReadResult, error) {
	var result SourceReadResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if wholeFileUnchanged(cp, fi) {
		return result, nil
	}
	events, err := src.FileParser(file)
	if err != nil {
		result.CaptureErrors = append(result.CaptureErrors, models.CaptureError{
			ID:              genID(),
			SourceName:      src.Name,
			SourceFile:      file,
			ErrorClass:      "parse_error",
			ErrorMessage:    err.Error(),
			ContextFragment: file,
		})
		return result, nil
	}
	for i := range events {
		applyWatchSourceMetadata(&events[i], src, cp)
	}
	PropagateModel(events)
	events = DeduplicateTokens(events)
	start := wholeFileCheckpointEventIndex(cp, fi)
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
		StateJSON:        encodeWholeFileCheckpointState(fi, end, complete),
	}
	return result, nil
}

func wholeFileUnchanged(cp *models.Checkpoint, fi os.FileInfo) bool {
	if cp == nil {
		return false
	}
	if inode := fileInode(fi); inode > 0 && cp.SourceInode > 0 && inode != cp.SourceInode {
		return false
	}
	var state wholeFileCheckpointState
	if err := json.Unmarshal([]byte(cp.StateJSON), &state); err != nil {
		return false
	}
	return wholeFileCheckpointMatches(state, fi) && state.Complete
}

func wholeFileCheckpointEventIndex(cp *models.Checkpoint, fi os.FileInfo) int {
	if cp == nil {
		return 0
	}
	var state wholeFileCheckpointState
	if err := json.Unmarshal([]byte(cp.StateJSON), &state); err != nil {
		return 0
	}
	if !wholeFileCheckpointMatches(state, fi) || state.Complete {
		return 0
	}
	if state.EventIndex < 0 {
		return 0
	}
	return state.EventIndex
}

func wholeFileCheckpointMatches(state wholeFileCheckpointState, fi os.FileInfo) bool {
	return state.Version == 1 && state.Size == fi.Size() && state.ModTimeUnixNS == fi.ModTime().UnixNano()
}

func wholeFileCheckpointOffset(fi os.FileInfo, complete bool) int64 {
	if complete {
		return fi.Size()
	}
	return 0
}

func encodeWholeFileCheckpointState(fi os.FileInfo, eventIndex int, complete bool) string {
	data, err := json.Marshal(wholeFileCheckpointState{
		Version:       1,
		Size:          fi.Size(),
		ModTimeUnixNS: fi.ModTime().UnixNano(),
		EventIndex:    eventIndex,
		Complete:      complete,
	})
	if err != nil {
		return ""
	}
	return string(data)
}

func sourceReadWindowFull(maxRecords, visibleRecords int) bool {
	return maxRecords > 0 && visibleRecords >= maxRecords
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
