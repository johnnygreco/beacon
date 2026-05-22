package store

import (
	"context"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func (s *Store) UpsertCheckpoint(ctx context.Context, cp models.Checkpoint) error {
	return s.insertCheckpoints(ctx, []models.Checkpoint{cp})
}

func (s *Store) LoadCheckpoints(ctx context.Context, sourceName string) (map[string]*models.Checkpoint, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT source_file,
		        argMax(source_inode, updated_at),
		        argMax(source_generation, updated_at),
		        argMax(last_offset, updated_at),
		        argMax(last_line_no, updated_at),
		        argMax(state_json, updated_at)
		 FROM capture_checkpoints
		 WHERE source_name = ?
		 GROUP BY source_file`, sourceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*models.Checkpoint)
	for rows.Next() {
		var sourceFile string
		var inode, offset uint64
		var generation, lineNo uint32
		var stateJSON string
		if err := rows.Scan(&sourceFile, &inode, &generation, &offset, &lineNo, &stateJSON); err != nil {
			continue
		}
		result[sourceFile] = &models.Checkpoint{
			SourceName:       sourceName,
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
		source_name, source_file, source_inode, source_generation,
		last_offset, last_line_no, state_json, updated_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, cp := range checkpoints {
		if err := batch.Append(
			cp.SourceName,
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
