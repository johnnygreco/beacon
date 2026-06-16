package store

import (
	"context"
	"fmt"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func (s *Store) UpsertCheckpoint(ctx context.Context, cp models.Checkpoint) error {
	return s.insertCheckpoints(ctx, []models.Checkpoint{cp})
}

func (s *Store) LoadCheckpoints(ctx context.Context, sourceName string) (map[string]*models.Checkpoint, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT if(source_file_key = '', source_file, source_file_key) AS file_key,
		        argMax(source_file, updated_at),
		        argMax(source_inode, updated_at),
		        argMax(source_generation, updated_at),
		        argMax(last_offset, updated_at),
		        argMax(last_line_no, updated_at),
		        argMax(state_json, updated_at)
		 FROM capture_checkpoints
		 WHERE source_name = ?
		 GROUP BY file_key`, sourceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*models.Checkpoint)
	for rows.Next() {
		var sourceFileKey, sourceFile string
		var inode, offset uint64
		var generation, lineNo uint32
		var stateJSON string
		if err := rows.Scan(&sourceFileKey, &sourceFile, &inode, &generation, &offset, &lineNo, &stateJSON); err != nil {
			return nil, fmt.Errorf("scan checkpoint: %w", err)
		}
		if sourceFileKey == "" {
			sourceFileKey = models.CheckpointSourceFileKey(sourceName, sourceFile)
		}
		result[sourceFileKey] = &models.Checkpoint{
			SourceName:       sourceName,
			SourceFileKey:    sourceFileKey,
			SourceFile:       sourceFile,
			SourceInode:      int64(inode),
			SourceGeneration: int(generation),
			LastOffset:       int64(offset),
			LastLineNo:       int(lineNo),
			StateJSON:        stateJSON,
		}
	}
	return result, rows.Err()
}

func (s *Store) insertCheckpoints(ctx context.Context, checkpoints []models.Checkpoint) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO capture_checkpoints (
		source_name, source_file_key, source_file, source_inode, source_generation,
		last_offset, last_line_no, state_json, updated_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, cp := range checkpoints {
		sourceFileKey := cp.EffectiveSourceFileKey()
		if err := batch.Append(
			cp.SourceName,
			sourceFileKey,
			cp.SourceFile,
			uint64(nonNegativeInt64(cp.SourceInode)),
			uint32(nonNegativeInt(cp.SourceGeneration)),
			uint64(nonNegativeInt64(cp.LastOffset)),
			uint32(nonNegativeInt(cp.LastLineNo)),
			cp.StateJSON,
			now,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}
