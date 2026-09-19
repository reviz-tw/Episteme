package app

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/config"
	"github.com/reviz-tw/Episteme/internal/inference"
	"github.com/reviz-tw/Episteme/internal/retrieval"
	"github.com/reviz-tw/Episteme/internal/storage"
	"github.com/reviz-tw/Episteme/internal/storage/db"
)

type App struct {
	Config  config.Config
	Store   *storage.Store
	TEI     *inference.TEI
	Vectors *storage.Qdrant
	Search  *retrieval.Service
}

func Open(ctx context.Context) (*App, error) {
	c := config.Load()
	if c.Dimension < 1 || c.Dimension > 65536 {
		return nil, fmt.Errorf("invalid embedding dimension")
	}
	s, e := storage.Open(ctx, c.DatabaseURL)
	if e != nil {
		return nil, e
	}
	e = s.Transaction(ctx, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE document_chunks SET is_dirty=true WHERE indexed_revision>0 AND indexed_model<>$1`, c.ModelKey()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE documents SET status='stale' WHERE id IN (SELECT document_id FROM document_chunks WHERE indexed_revision>0 AND indexed_model<>$1 AND NOT is_excluded)`, c.ModelKey()); e != nil {
			return e
		}
		return db.New(tx).PurgeExpiredSessions(ctx)
	})
	if e != nil {
		s.Pool.Close()
		return nil, e
	}
	v, e := storage.NewQdrant(c)
	if e != nil {
		s.Pool.Close()
		return nil, e
	}
	t := inference.New(c.EmbedURL, c.RerankURL, c.Model, c.Dimension)
	return &App{c, s, t, v, &retrieval.Service{Store: s, Vectors: v, Model: t, ModelKey: c.ModelKey()}}, nil
}
func (a *App) Close() { a.Vectors.Client.Close(); a.Store.Pool.Close() }
