package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/auth"
	"github.com/reviz-tw/Episteme/internal/metadata"
	"net/http"
	"sync"
	"time"
)

func (s *Server) jobAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string `json:"action"`
	}
	if e := Decode(w, r, &in); e != nil {
		Fail(w, e)
		return
	}
	if in.Action != "pause" && in.Action != "resume" && in.Action != "cancel" {
		Fail(w, errors.New("不支援的操作"))
		return
	}
	e := s.Store.Transaction(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		// Match the worker's task -> job lock order to avoid resume deadlocks.
		if in.Action == "resume" {
			if _, e := tx.Exec(ctx, `UPDATE indexing_tasks SET attempts=0,available_at=now() WHERE job_id=$1 AND NOT done`, r.PathValue("id")); e != nil {
				return e
			}
		}
		var status string
		if e := tx.QueryRow(ctx, `SELECT status FROM indexing_jobs WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&status); e != nil {
			return e
		}
		if status == "completed" || status == "cancelled" {
			return errors.New("工作已結束")
		}
		next := "queued"
		if in.Action == "pause" {
			next = "paused"
		}
		if in.Action == "cancel" {
			next = "cancelled"
		}
		if _, e := tx.Exec(ctx, `UPDATE indexing_jobs SET status=$2,error='',updated_at=now() WHERE id=$1`, r.PathValue("id"), next); e != nil {
			return e
		}
		return nil
	})
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		Fail(w, errors.New("SSE 不可用"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		cookie, e := r.Cookie("episteme_session")
		if e != nil {
			return
		}
		if _, e = auth.Username(r.Context(), s.Store, cookie.Value); e != nil {
			return
		}
		jobs, e := s.Store.Jobs(r.Context())
		if e != nil {
			return
		}
		b, _ := json.Marshal(jobs)
		if _, e = fmt.Fprintf(w, "event: progress\ndata: %s\n\n", b); e != nil {
			return
		}
		f.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
	}
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	docs, e := s.Store.Documents(r.Context())
	if e != nil {
		Fail(w, e)
		return
	}
	var indexed int
	if e = s.Store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM document_chunks WHERE indexed_revision>0 AND NOT is_excluded AND indexed_model=$1`, s.Config.ModelKey()).Scan(&indexed); e != nil {
		Fail(w, e)
		return
	}
	drafts, tokens := 0, 0
	for _, d := range docs {
		drafts += d.ChunkCount - d.ReviewedCount
		tokens += d.TotalTokens
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var embed, rerank, qok, metadataOK bool
	var points uint64
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); embed = s.TEI.Health(ctx, s.Config.EmbedURL) }()
	go func() { defer wg.Done(); rerank = s.TEI.Health(ctx, s.Config.RerankURL) }()
	go func() { defer wg.Done(); var err error; points, err = s.Vectors.Health(ctx); qok = err == nil }()
	go func() {
		defer wg.Done()
		if s.Config.MetadataEnabled {
			metadataOK = metadata.New(s.Config.MetadataURL).Health(ctx, s.Config.MetadataModel)
		}
	}()
	wg.Wait()
	JSON(w, 200, map[string]any{"documents": len(docs), "indexed_chunks": indexed, "draft_chunks": drafts, "total_tokens": tokens, "vector_bytes_estimate": points * uint64(s.Config.Dimension) * 4, "health": map[string]bool{"embedding": embed, "rerank": rerank, "qdrant": qok, "metadata": metadataOK}, "metadata_enabled": s.Config.MetadataEnabled, "metadata_model": s.Config.MetadataModel, "model": s.Config.Model, "dimension": s.Config.Dimension, "collection": s.Config.CollectionName()})
}
