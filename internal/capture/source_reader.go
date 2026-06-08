package capture

import (
	"bufio"
	"context"
	"log/slog"
	"os"

	"github.com/johnnygreco/beacon/internal/models"
)

type SourceReadResult struct {
	Events        []NormalizedEvent
	CaptureErrors []models.CaptureError
	Checkpoint    *models.Checkpoint
}

func ReadSourceFile(ctx context.Context, src WatchSource, file string, cp *models.Checkpoint, logger *slog.Logger) (SourceReadResult, error) {
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
		return readWholeSourceFile(ctx, src, file, fi, cp)
	}
	return readLineSourceFile(ctx, src, file, fi, cp, logger)
}

func readLineSourceFile(ctx context.Context, src WatchSource, file string, fi os.FileInfo, cp *models.Checkpoint, logger *slog.Logger) (SourceReadResult, error) {
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

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, scannerMaxTokenBytes), scannerMaxTokenBytes)

	var allEvents []NormalizedEvent
	var replayLines []replayLine

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		lineNo++
		lineBytes := scanner.Bytes()
		lineLen := int64(len(lineBytes)) + 1
		replayLines = appendReplayLine(replayLines, replayLine{offset: offset, lineNo: lineNo}, incrementalReplayLines)

		if len(lineBytes) == 0 {
			offset += lineLen
			continue
		}

		events, err := src.Parser(lineBytes, file, lineNo, offset)
		if err != nil {
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
			continue
		}

		for i := range events {
			applyWatchSourceMetadata(&events[i], src, cp)
		}
		allEvents = append(allEvents, events...)
		offset += lineLen
	}
	if err := scanner.Err(); err != nil {
		return result, err
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

func readWholeSourceFile(ctx context.Context, src WatchSource, file string, fi os.FileInfo, cp *models.Checkpoint) (SourceReadResult, error) {
	var result SourceReadResult
	if err := ctx.Err(); err != nil {
		return result, err
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
	result.Events = DeduplicateTokens(events)

	sourceGeneration := 0
	if cp != nil {
		sourceGeneration = cp.SourceGeneration
	}
	result.Checkpoint = &models.Checkpoint{
		SourceName:       src.Name,
		SourceFile:       file,
		SourceInode:      fileInode(fi),
		SourceGeneration: sourceGeneration,
		LastOffset:       0,
		LastLineNo:       0,
	}
	return result, nil
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
