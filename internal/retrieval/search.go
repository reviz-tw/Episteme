package retrieval

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/storage"
	"math"
	"sort"
	"strings"
)

type Inference interface {
	Embed(context.Context, string) ([]float32, error)
	Rerank(context.Context, string, []string) (map[int]float64, error)
}

var ErrInvalidInput = errors.New("搜尋參數不合法")

type Service struct {
	Store    *storage.Store
	Vectors  storage.VectorStore
	Model    Inference
	ModelKey string
}
type Input struct {
	Query               string   `json:"query"`
	Alpha               float64  `json:"alpha"`
	Rerank              bool     `json:"rerank_enabled"`
	Limit               int      `json:"limit,omitempty"`
	MinScore            *float64 `json:"min_score,omitempty"`
	IncludeLowRelevance bool     `json:"include_low_relevance,omitempty"`
}
type Result struct {
	ChunkID          string                   `json:"chunk_id"`
	DocumentID       string                   `json:"document_id"`
	Filename         string                   `json:"filename"`
	Content          string                   `json:"content"`
	Breadcrumbs      []string                 `json:"breadcrumbs"`
	Score            float64                  `json:"score"`
	DenseScore       float64                  `json:"dense_score"`
	SparseScore      float64                  `json:"sparse_score"`
	RerankScore      *float64                 `json:"rerank_score"`
	InitialRank      int                      `json:"initial_rank"`
	Rank             int                      `json:"rank"`
	MatchType        string                   `json:"match_type"`
	DocumentMetadata storage.DocumentMetadata `json:"document_metadata"`
}

func (s *Service) Search(ctx context.Context, in Input) ([]Result, error) {
	if strings.TrimSpace(in.Query) == "" || len(in.Query) > 8000 || math.IsNaN(in.Alpha) || in.Alpha < 0 || in.Alpha > 1 {
		return nil, fmt.Errorf("%w：query 或 alpha 不合法", ErrInvalidInput)
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 100 {
		return nil, fmt.Errorf("%w：limit 必須介於 1–100", ErrInvalidInput)
	}
	if in.MinScore != nil && (math.IsNaN(*in.MinScore) || *in.MinScore < 0 || *in.MinScore > 1) {
		return nil, fmt.Errorf("%w：min_score 必須介於 0–1", ErrInvalidInput)
	}
	type candidate struct {
		r        Result
		revision int
	}
	candidates := map[string]*candidate{}
	add := func(hits []storage.Hit, weight float64, dense bool) {
		for i, h := range hits {
			c := candidates[h.ID]
			if c == nil {
				c = &candidate{r: Result{ChunkID: h.ID}, revision: h.Revision}
				candidates[h.ID] = c
			}
			c.r.Score += weight / (60 + float64(i+1))
			if dense {
				c.r.DenseScore = h.Score
			} else {
				c.r.SparseScore = h.Score
			}
		}
	}
	if in.Alpha > 0 {
		v, e := s.Model.Embed(ctx, in.Query)
		if e != nil {
			return nil, e
		}
		hits, e := s.Vectors.Search(ctx, "dense", v, nil, nil, 100)
		if e != nil {
			return nil, e
		}
		add(hits, in.Alpha, true)
	}
	if in.Alpha < 1 {
		ids, values := Sparse(in.Query, true)
		hits, e := s.Vectors.Search(ctx, "sparse", nil, ids, values, 100)
		if e != nil {
			return nil, e
		}
		add(hits, 1-in.Alpha, false)
	}
	out := []Result{}
	for id, c := range candidates {
		var chunk storage.Chunk
		e := storage.JSONRow(s.Store.Pool.QueryRow(ctx, `SELECT to_jsonb(c) FROM document_chunks c WHERE id=$1 AND NOT is_excluded AND indexed_revision=$2 AND indexed_model=$3`, id, c.revision, s.ModelKey), &chunk)
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			return nil, e
		}
		c.r.DocumentID = chunk.DocumentID
		c.r.Content = chunk.IndexedContent
		c.r.Breadcrumbs = chunk.IndexedBreadcrumbs
		c.r.DocumentMetadata = chunk.IndexedDocumentMetadata
		if e = s.Store.Pool.QueryRow(ctx, `SELECT filename FROM documents WHERE id=$1`, chunk.DocumentID).Scan(&c.r.Filename); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			return nil, e
		}
		out = append(out, c.r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ChunkID < out[j].ChunkID
		}
		return out[i].Score > out[j].Score
	})
	// Keep a reranking pool even when the caller only wants a few final results.
	poolSize := max(20, in.Limit)
	if len(out) > poolSize {
		out = out[:poolSize]
	}
	for i := range out {
		out[i].InitialRank = i + 1
		out[i].Rank = i + 1
	}
	if in.Rerank && len(out) > 0 {
		texts := make([]string, len(out))
		for i, r := range out {
			texts[i] = r.Content
		}
		scores, e := s.Model.Rerank(ctx, in.Query, texts)
		if e != nil {
			return nil, e
		}
		for i := range out {
			score := scores[i]
			out[i].RerankScore = &score
		}
		sort.SliceStable(out, func(i, j int) bool { return *out[i].RerankScore > *out[j].RerankScore })
	}
	return relevantResults(out, in), nil
}
