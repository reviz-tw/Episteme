package api

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/chunker"
	"github.com/reviz-tw/Episteme/internal/storage"
	"net/http"
)

type edit struct {
	ID          string    `json:"id"`
	Revision    int       `json:"revision"`
	Raw         *string   `json:"raw_markdown"`
	Breadcrumbs *[]string `json:"breadcrumbs"`
	FAQ         *[]string `json:"faq"`
	Reviewed    *bool     `json:"is_reviewed"`
	Excluded    *bool     `json:"is_excluded"`
}

type metadataEdit struct {
	DocumentRevision int                       `json:"document_revision,omitempty"`
	DocumentMetadata *storage.DocumentMetadata `json:"document_metadata,omitempty"`
}

func (s *Server) saveEdits(ctx context.Context, doc string, edits []edit, metadata metadataEdit) error {
	if metadata.DocumentMetadata != nil {
		if metadata.DocumentRevision < 1 {
			return errors.New("儲存文件資料需要文件版本")
		}
		normalized, err := storage.NormalizeMetadata(*metadata.DocumentMetadata)
		if err != nil {
			return err
		}
		metadata.DocumentMetadata = &normalized
	} else if metadata.DocumentRevision != 0 {
		return errors.New("請提供文件資料")
	}
	return s.Store.Transaction(ctx, func(tx pgx.Tx) error {
		if e := storage.LockDocument(ctx, tx, doc); e != nil {
			return e
		}
		chunks, e := storage.Chunks(ctx, tx, doc)
		if e != nil {
			return e
		}
		byID := map[string]storage.Chunk{}
		for _, c := range chunks {
			byID[c.ID] = c
		}
		seen := map[string]bool{}
		for _, v := range edits {
			c, ok := byID[v.ID]
			if !ok {
				return pgx.ErrNoRows
			}
			if seen[v.ID] {
				return errors.New("重複的切片")
			}
			seen[v.ID] = true
			if v.Revision != c.Revision {
				return storage.ErrConflict
			}
			before := c.Content
			wasExcluded := c.Excluded
			if v.Raw != nil {
				c.Raw = *v.Raw
			}
			if v.Breadcrumbs != nil {
				c.Breadcrumbs = *v.Breadcrumbs
			}
			if v.FAQ != nil {
				c.Metadata.FAQ = *v.FAQ
			}
			if before != chunker.Content(c.Raw, c.Breadcrumbs, c.Metadata.FAQ) {
				c.Reviewed = false
			}
			if v.Reviewed != nil {
				c.Reviewed = *v.Reviewed
			}
			if v.Excluded != nil {
				c.Excluded = *v.Excluded
			}
			if e = validation(c); e != nil {
				return e
			}
			if e = storage.SaveChunk(ctx, tx, c); e != nil {
				return e
			}
			if c.Excluded && !wasExcluded {
				job, e := storage.NewJob(ctx, tx, "exclude")
				if e != nil {
					return e
				}
				if e = s.queueDelete(ctx, tx, job, doc, c.ID); e != nil {
					return e
				}
			}
		}
		if metadata.DocumentMetadata != nil {
			if e := storage.SaveDocumentMetadata(ctx, tx, doc, metadata.DocumentRevision, *metadata.DocumentMetadata); e != nil {
				return e
			}
		}
		return storage.RefreshDocument(ctx, tx, doc)
	})
}
func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Chunks []edit `json:"chunks"`
		metadataEdit
	}
	if e := Decode(w, r, &in); e != nil {
		Fail(w, e)
		return
	}
	if (len(in.Chunks) == 0 && in.DocumentMetadata == nil) || len(in.Chunks) > 1000 {
		Fail(w, errors.New("請提供文件資料或 1–1000 個切片"))
		return
	}
	if e := s.saveEdits(r.Context(), r.PathValue("id"), in.Chunks, in.metadataEdit); e != nil {
		Fail(w, e)
		return
	}
	s.chunks(w, r)
}
func (s *Server) patchChunk(w http.ResponseWriter, r *http.Request) {
	var v edit
	if e := Decode(w, r, &v); e != nil {
		Fail(w, e)
		return
	}
	v.ID = r.PathValue("id")
	var doc string
	if e := s.Store.Pool.QueryRow(r.Context(), `SELECT document_id::text FROM document_chunks WHERE id=$1`, v.ID).Scan(&doc); e != nil {
		Fail(w, e)
		return
	}
	if e := s.saveEdits(r.Context(), doc, []edit{v}, metadataEdit{}); e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 200, map[string]bool{"saved": true})
}
func (s *Server) splitChunk(w http.ResponseWriter, r *http.Request) { s.structure(w, r, false) }
func (s *Server) mergeChunk(w http.ResponseWriter, r *http.Request) { s.structure(w, r, true) }
func (s *Server) structure(w http.ResponseWriter, r *http.Request, merge bool) {
	var in struct {
		Position     int `json:"position"`
		Revision     int `json:"revision"`
		NextRevision int `json:"next_revision,omitempty"`
	}
	if e := Decode(w, r, &in); e != nil {
		Fail(w, e)
		return
	}
	id := r.PathValue("id")
	var doc string
	if e := s.Store.Pool.QueryRow(r.Context(), `SELECT document_id::text FROM document_chunks WHERE id=$1`, id).Scan(&doc); e != nil {
		Fail(w, e)
		return
	}
	e := s.Store.Transaction(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := storage.LockDocument(ctx, tx, doc); e != nil {
			return e
		}
		chunks, e := storage.Chunks(ctx, tx, doc)
		if e != nil {
			return e
		}
		index := -1
		for i, c := range chunks {
			if c.ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return pgx.ErrNoRows
		}
		c := chunks[index]
		if c.Revision != in.Revision {
			return storage.ErrConflict
		}
		c.Reviewed = false
		if merge {
			if index+1 >= len(chunks) {
				return errors.New("沒有下一個切片")
			}
			next := chunks[index+1]
			if next.Revision != in.NextRevision {
				return storage.ErrConflict
			}
			if c.Excluded != next.Excluded {
				return errors.New("請先讓兩個切片的排除狀態一致")
			}
			c.Raw += "\n\n" + next.Raw
			c.Metadata.FAQ = append(c.Metadata.FAQ, next.Metadata.FAQ...)
			if e = validation(c); e != nil {
				return e
			}
			job, e := storage.NewJob(ctx, tx, "merge_cleanup")
			if e != nil {
				return e
			}
			if e = s.queueDelete(ctx, tx, job, doc, next.ID); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `DELETE FROM document_chunks WHERE id=$1`, next.ID); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE document_chunks SET chunk_index=chunk_index-1 WHERE document_id=$1 AND chunk_index>$2`, doc, next.Index); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE document_chunks SET source_end=$2 WHERE id=$1`, c.ID, next.End); e != nil {
				return e
			}
		} else {
			if c.Excluded {
				return errors.New("請先取消排除再切分")
			}
			left, right, ok := chunker.SplitAt(c.Raw, in.Position)
			if !ok {
				return errors.New("請將游標放在切片內容中間")
			}
			c.Raw = left
			if _, e = tx.Exec(ctx, `UPDATE document_chunks SET chunk_index=chunk_index+1 WHERE document_id=$1 AND chunk_index>$2`, doc, c.Index); e != nil {
				return e
			}
			next := c
			next.ID = uuid.NewString()
			next.Index++
			next.Raw = right
			next.Metadata.FAQ = nil
			if e = storage.InsertChunk(ctx, tx, next); e != nil {
				return e
			}
		}
		if e = storage.SaveChunk(ctx, tx, c); e != nil {
			return e
		}
		return storage.RefreshDocument(ctx, tx, doc)
	})
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 200, map[string]bool{"saved": true})
}
