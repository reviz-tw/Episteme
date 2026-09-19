package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/storage"
)

type Worker struct {
	Store     *storage.Store
	Extractor Extractor
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
				worked, err := w.Step(ctx)
				if err != nil {
					slog.Error("metadata job", "error", err)
				}
				if !worked || err != nil {
					break
				}
			}
		}
	}
}

// Claims are short database transactions. The model runs without locking a
// document; a lease plus token allows restart recovery without stale writes.
func (w *Worker) Step(parent context.Context) (bool, error) {
	token := uuid.NewString()
	var id, model string
	var base, attempts int
	err := w.Store.Pool.QueryRow(parent, `UPDATE metadata_jobs SET status='running',attempts=attempts+1,run_token=$1,available_at=now()+interval '3 minutes',updated_at=now()
 WHERE document_id=(SELECT document_id FROM metadata_jobs WHERE status IN ('queued','running') AND available_at<=now() ORDER BY available_at FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING document_id::text,model,base_revision,attempts`, token).Scan(&id, &model, &base, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var source string
	err = w.Store.Pool.QueryRow(parent, `SELECT source_markdown FROM documents WHERE id=$1`, id).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	ctx, cancel := context.WithTimeout(parent, 125*time.Second)
	result, modelErr := w.Extractor.Extract(ctx, source, model)
	cancel()
	if parent.Err() != nil {
		recovery, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, err = w.Store.Pool.Exec(recovery, `UPDATE metadata_jobs SET status='queued',available_at=now(),updated_at=now() WHERE document_id=$1 AND run_token=$2`, id, token)
		return true, err
	}
	if modelErr != nil {
		state := "queued"
		if attempts >= 3 {
			state = "failed"
		}
		_, err = w.Store.Pool.Exec(parent, `UPDATE metadata_jobs SET status=$3,error=$4,available_at=now()+($5*interval '1 second'),updated_at=now() WHERE document_id=$1 AND run_token=$2`, id, token, state, modelErr.Error(), 10*attempts)
		return true, err
	}
	err = w.Store.Transaction(parent, func(tx pgx.Tx) error {
		if err := storage.LockDocument(parent, tx, id); errors.Is(err, pgx.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		var currentToken string
		if err := tx.QueryRow(parent, `SELECT coalesce(run_token::text,'') FROM metadata_jobs WHERE document_id=$1 FOR UPDATE`, id).Scan(&currentToken); err != nil {
			return err
		}
		if currentToken != token {
			return nil
		}
		var revision int
		var raw []byte
		if err := tx.QueryRow(parent, `SELECT metadata_revision,metadata FROM documents WHERE id=$1`, id).Scan(&revision, &raw); err != nil {
			return err
		}
		state := "completed"
		message := ""
		if revision != base {
			state = "skipped"
		} else {
			var current storage.DocumentMetadata
			if err := json.Unmarshal(raw, &current); err != nil {
				return err
			}
			merged, err := storage.NormalizeMetadata(storage.FillMetadata(current, result.Metadata))
			if err != nil {
				state, message = "failed", "合併文件資料失敗："+err.Error()
			} else {
				if err = storage.ApplyDocumentMetadata(parent, tx, id, merged); err != nil {
					return err
				}
				if err = storage.RefreshDocument(parent, tx, id); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(parent, `UPDATE metadata_jobs SET status=$3,result=$4,sampled=$5,error=$6,updated_at=now() WHERE document_id=$1 AND run_token=$2`, id, token, state, result.Metadata, result.Sampled, message)
		return err
	})
	return true, err
}
