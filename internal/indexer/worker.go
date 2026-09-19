package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/retrieval"
	"github.com/reviz-tw/Episteme/internal/storage"
	"log/slog"
	"time"
)

type Worker struct {
	Store                *storage.Store
	Vectors              storage.VectorStore
	Model                retrieval.Inference
	ModelKey, Collection string
}

func (w *Worker) Run(ctx context.Context) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			for {
				worked, e := w.Step(ctx)
				if e != nil {
					slog.Error("index task", "error", e)
				}
				if !worked || e != nil {
					break
				}
			}
		}
	}
}
func (w *Worker) Step(parent context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, 110*time.Second)
	defer cancel()
	worked := false
	e := w.Store.Transaction(ctx, func(tx pgx.Tx) error {
		var task int64
		var job, doc, id, kind, collection string
		var attempts int
		var metadata []byte
		e := tx.QueryRow(ctx, `SELECT t.id,t.job_id::text,t.document_id::text,t.chunk_id::text,t.kind,t.collection,t.attempts,t.document_metadata FROM indexing_tasks t JOIN indexing_jobs j ON j.id=t.job_id WHERE NOT t.done AND t.available_at<=now() AND j.status IN ('queued','running') ORDER BY t.id FOR UPDATE OF t SKIP LOCKED LIMIT 1`).Scan(&task, &job, &doc, &id, &kind, &collection, &attempts, &metadata)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		worked = true
		if e = storage.LockDocument(ctx, tx, doc); e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		// All edits and publication for this document serialize on the same row lock.
		// A crash after a Qdrant write leaves this task pending; the idempotent retry
		// repairs it. Search only accepts the revision committed to PostgreSQL.
		var opErr error
		if kind == "delete" {
			// A delayed exclusion cleanup must not erase a later restored publication.
			var included bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM document_chunks WHERE id=$1 AND NOT is_excluded)`, id).Scan(&included); e != nil {
				return e
			}
			if !included {
				opErr = w.Vectors.Delete(ctx, id, collection)
			}
		} else {
			var c storage.Chunk
			e = storage.JSONRow(tx.QueryRow(ctx, `SELECT to_jsonb(c) FROM document_chunks c WHERE id=$1`, id), &c)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if e == nil && !c.Excluded && c.Reviewed {
				if e := json.Unmarshal(metadata, &c.DocumentMetadata); e != nil {
					return e
				}
				if collection != w.Collection {
					opErr = fmt.Errorf("model changed; cancel old job and rebuild with current model")
				} else {
					opErr = w.Vectors.Ensure(ctx)
					if opErr == nil {
						var dense []float32
						dense, opErr = w.Model.Embed(ctx, c.Content)
						if opErr == nil {
							ids, values := retrieval.Sparse(c.Content, false)
							opErr = w.Vectors.Upsert(ctx, c, dense, ids, values)
						}
					}
					if opErr == nil {
						_, opErr = tx.Exec(ctx, `UPDATE document_chunks SET is_dirty=(SELECT metadata FROM documents WHERE id=$4) IS DISTINCT FROM $3::jsonb,qdrant_point_id=id,indexed_revision=revision,indexed_content=content,indexed_breadcrumbs=breadcrumbs,indexed_model=$2,indexed_document_metadata=$3 WHERE id=$1`, id, w.ModelKey, metadata, doc)
					}
				}
			} else if e == nil && !c.Excluded && !c.Reviewed {
				opErr = storage.ErrReview
			}
		}
		if opErr != nil {
			attempts++
			status := "queued"
			if attempts >= 5 {
				status = "failed"
			}
			if _, e = tx.Exec(ctx, `UPDATE indexing_tasks SET attempts=$2,available_at=now()+($3*interval '1 second') WHERE id=$1`, task, attempts, 1<<attempts); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE indexing_jobs SET status=CASE WHEN status IN ('paused','cancelled') THEN status ELSE $2 END,error=$3,updated_at=now() WHERE id=$1`, job, status, opErr.Error()); e != nil {
				return e
			}
			if status == "failed" {
				_, e = tx.Exec(ctx, `UPDATE documents SET status='failed' WHERE id=$1`, doc)
			}
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE indexing_tasks SET done=true WHERE id=$1`, task); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE indexing_jobs SET completed=completed+1,status=CASE WHEN status IN ('paused','cancelled') THEN status WHEN completed+1=total THEN 'completed' ELSE 'running' END,error='',updated_at=now() WHERE id=$1`, job); e != nil {
			return e
		}
		return storage.RefreshDocument(ctx, tx, doc)
	})
	return worked, e
}
