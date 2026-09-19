package storage

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

var ErrMetadataPending = errors.New("文件資料仍在自動擷取，請稍後再試")

type MetadataGeneration struct {
	Status    string `json:"status"`
	Model     string `json:"model"`
	Attempts  int    `json:"attempts"`
	Error     string `json:"error"`
	Sampled   bool   `json:"sampled"`
	UpdatedAt string `json:"updated_at"`
}

// Caller holds the document lock. One durable job per document prevents duplicate
// requests; run_token fences a slow worker after a lease expires or job is retried.
func QueueMetadata(ctx context.Context, tx pgx.Tx, id, model string) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM metadata_jobs WHERE document_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return ErrMetadataPending
	}
	_, err := tx.Exec(ctx, `INSERT INTO metadata_jobs(document_id,model,base_revision)
 SELECT id,$2,metadata_revision FROM documents WHERE id=$1
 ON CONFLICT(document_id) DO UPDATE SET status='queued',model=EXCLUDED.model,base_revision=EXCLUDED.base_revision,attempts=0,error='',sampled=false,result='{}',run_token=NULL,available_at=now(),created_at=now(),updated_at=now()`, id, model)
	return err
}

// Only fill blanks. Existing fields, tags and all custom user fields survive retries.
func FillMetadata(current, suggested DocumentMetadata) DocumentMetadata {
	if current.Title == "" {
		current.Title = suggested.Title
	}
	if current.Source == "" {
		current.Source = suggested.Source
	}
	if current.SourceURL == "" {
		current.SourceURL = suggested.SourceURL
	}
	if current.Author == "" {
		current.Author = suggested.Author
	}
	if current.PublishedAt == "" {
		current.PublishedAt = suggested.PublishedAt
	}
	if len(current.Tags) == 0 {
		current.Tags = suggested.Tags
	}
	return current
}
