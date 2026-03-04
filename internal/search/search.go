package search

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Result is the output of a search query.
type Result struct {
	DocumentID string
	SessionID  string
	DocType    string
	Content    string
	Snippet    string
	Score      float64
	Source     string
	CreatedAt  time.Time
}

// Searcher performs keyword and semantic search with RRF ranking.
type Searcher struct {
	db       *sql.DB
	provider EmbeddingProvider
}

// NewSearcher creates a new searcher.
func NewSearcher(db *sql.DB, provider EmbeddingProvider) *Searcher {
	return &Searcher{db: db, provider: provider}
}

// Search performs combined keyword + semantic search with Reciprocal Rank Fusion.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 20
	}

	// Run keyword and semantic searches in parallel
	type resultSet struct {
		results []scoredDoc
		err     error
	}

	keywordCh := make(chan resultSet, 1)
	semanticCh := make(chan resultSet, 1)

	go func() {
		results, err := s.keywordSearch(ctx, query, limit*2)
		keywordCh <- resultSet{results, err}
	}()

	go func() {
		results, err := s.semanticSearch(ctx, query, limit*2)
		semanticCh <- resultSet{results, err}
	}()

	keywordRes := <-keywordCh
	semanticRes := <-semanticCh

	// Combine results with RRF
	return s.rrfMerge(keywordRes.results, keywordRes.err, semanticRes.results, semanticRes.err, limit)
}

type scoredDoc struct {
	ID        string
	SessionID string
	DocType   string
	Content   string
	Source    string
	CreatedAt time.Time
	Score     float64
}

func (s *Searcher) keywordSearch(ctx context.Context, query string, limit int) ([]scoredDoc, error) {
	// Use ILIKE for case-insensitive keyword search
	pattern := "%" + strings.ReplaceAll(query, "%", "\\%") + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.id, d.session_id, d.doc_type, d.content, COALESCE(ses.source, ''), d.created_at
		 FROM documents d
		 LEFT JOIN sessions ses ON ses.id = d.session_id
		 WHERE d.content ILIKE $1
		 ORDER BY d.created_at DESC
		 LIMIT $2`,
		pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredDoc
	rank := 1
	for rows.Next() {
		var d scoredDoc
		if err := rows.Scan(&d.ID, &d.SessionID, &d.DocType, &d.Content, &d.Source, &d.CreatedAt); err != nil {
			continue
		}
		d.Score = float64(rank)
		rank++
		results = append(results, d)
	}
	return results, nil
}

func (s *Searcher) semanticSearch(ctx context.Context, query string, limit int) ([]scoredDoc, error) {
	if s.provider == nil {
		return nil, nil
	}

	embedding, err := s.provider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	// Format embedding as DuckDB array literal
	embStr := formatEmbedding(embedding)

	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(
			`SELECT d.id, d.session_id, d.doc_type, d.content, COALESCE(ses.source, ''), d.created_at,
			        list_cosine_similarity(d.embedding, %s::FLOAT[]) AS similarity
			 FROM documents d
			 LEFT JOIN sessions ses ON ses.id = d.session_id
			 WHERE d.embedding IS NOT NULL
			 ORDER BY similarity DESC
			 LIMIT $1`, embStr),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredDoc
	rank := 1
	for rows.Next() {
		var d scoredDoc
		var similarity float64
		if err := rows.Scan(&d.ID, &d.SessionID, &d.DocType, &d.Content, &d.Source, &d.CreatedAt, &similarity); err != nil {
			continue
		}
		d.Score = float64(rank)
		rank++
		results = append(results, d)
	}
	return results, nil
}

// rrfMerge combines keyword and semantic results using Reciprocal Rank Fusion (k=60).
func (s *Searcher) rrfMerge(keyword []scoredDoc, kwErr error, semantic []scoredDoc, semErr error, limit int) ([]Result, error) {
	const k = 60.0
	scores := make(map[string]float64)
	docs := make(map[string]scoredDoc)

	if kwErr == nil {
		for i, d := range keyword {
			scores[d.ID] += 1.0 / (k + float64(i+1))
			docs[d.ID] = d
		}
	}
	if semErr == nil {
		for i, d := range semantic {
			scores[d.ID] += 1.0 / (k + float64(i+1))
			docs[d.ID] = d
		}
	}

	if len(docs) == 0 {
		if kwErr != nil {
			return nil, kwErr
		}
		return nil, semErr
	}

	type ranked struct {
		id    string
		score float64
	}
	var ranked_list []ranked
	for id, score := range scores {
		ranked_list = append(ranked_list, ranked{id, score})
	}
	sort.Slice(ranked_list, func(i, j int) bool {
		return ranked_list[i].score > ranked_list[j].score
	})

	if len(ranked_list) > limit {
		ranked_list = ranked_list[:limit]
	}

	results := make([]Result, 0, len(ranked_list))
	for _, r := range ranked_list {
		d := docs[r.id]
		snippet := d.Content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		results = append(results, Result{
			DocumentID: d.ID,
			SessionID:  d.SessionID,
			DocType:    d.DocType,
			Content:    d.Content,
			Snippet:    snippet,
			Score:      math.Round(r.score*10000) / 10000,
			Source:     d.Source,
			CreatedAt:  d.CreatedAt,
		})
	}
	return results, nil
}

func formatEmbedding(emb []float32) string {
	var b strings.Builder
	b.WriteString("[")
	for i, v := range emb {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%f", v)
	}
	b.WriteString("]")
	return b.String()
}
