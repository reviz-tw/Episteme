package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/reviz-tw/Episteme/internal/chunker"
	"github.com/reviz-tw/Episteme/internal/ingest"
	"github.com/reviz-tw/Episteme/internal/storage"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
	if e := r.ParseMultipartForm(1 << 20); e != nil {
		Fail(w, errors.New("上傳失敗，檔案上限 20 MB"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, e := r.FormFile("file")
	if e != nil {
		Fail(w, e)
		return
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, (20<<20)+1))
	if e != nil || len(data) > 20<<20 {
		Fail(w, errors.New("檔案上限 20 MB"))
		return
	}
	name := filepath.Base(header.Filename)
	if len(name) > 255 {
		Fail(w, errors.New("檔名過長"))
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	source := string(data)
	mimeType := "text/markdown"
	if ext == ".pdf" {
		if !strings.HasPrefix(source, "%PDF-") {
			Fail(w, errors.New("PDF 格式不正確"))
			return
		}
		mimeType = "application/pdf"
		tmp, e := os.CreateTemp("", "episteme-*.pdf")
		if e != nil {
			Fail(w, e)
			return
		}
		path := tmp.Name()
		defer os.Remove(path)
		if _, e = tmp.Write(data); e != nil {
			tmp.Close()
			Fail(w, e)
			return
		}
		tmp.Close()
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		out, e := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", path, "-").Output()
		if e != nil {
			Fail(w, errors.New("PDF 解析失敗，請確認已安裝 pdftotext 且 PDF 未加密"))
			return
		}
		source = string(out)
	} else if ext == ".json" {
		mimeType = "application/json"
		source, e = ingest.JSONMarkdown(data)
		if e != nil {
			Fail(w, e)
			return
		}
	} else if ext != ".md" && ext != ".markdown" {
		Fail(w, errors.New("僅支援 Markdown、PDF 與 JSON"))
		return
	}
	if !utf8.ValidString(source) || strings.TrimSpace(source) == "" || len(source) > 20<<20 {
		Fail(w, errors.New("沒有可解析的文字；掃描 PDF 請先執行 OCR"))
		return
	}
	id := uuid.NewString()
	parts := chunker.Split(source, 512)
	e = s.Store.Transaction(r.Context(), func(tx pgx.Tx) error {
		_, e := tx.Exec(r.Context(), `INSERT INTO documents(id,filename,mime_type,storage_path,original,source_markdown)VALUES($1,$2,$3,$4,$5,$6)`, id, name, mimeType, "postgres://documents/"+id, data, source)
		if e != nil {
			return e
		}
		if e = storage.InsertParts(r.Context(), tx, id, parts); e != nil {
			return e
		}
		if s.Config.MetadataEnabled {
			return storage.QueueMetadata(r.Context(), tx, id, s.Config.MetadataModel)
		}
		return nil
	})
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 201, map[string]any{"id": id, "chunk_count": len(parts)})
}

func (s *Server) extractMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.Config.MetadataEnabled {
		Fail(w, errors.New("自動擷取尚未啟用"))
		return
	}
	id := r.PathValue("id")
	err := s.Store.Transaction(r.Context(), func(tx pgx.Tx) error {
		if err := storage.LockDocument(r.Context(), tx, id); err != nil {
			return err
		}
		return storage.QueueMetadata(r.Context(), tx, id, s.Config.MetadataModel)
	})
	if err != nil {
		Fail(w, err)
		return
	}
	JSON(w, 202, map[string]string{"status": "queued"})
}
func (s *Server) original(w http.ResponseWriter, r *http.Request) {
	var name, mimeType string
	var data []byte
	if e := s.Store.Pool.QueryRow(r.Context(), `SELECT filename,mime_type,original FROM documents WHERE id=$1`, r.PathValue("id")).Scan(&name, &mimeType, &data); e != nil {
		Fail(w, e)
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	_, _ = w.Write(data)
}

func (s *Server) patchDocument(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int                       `json:"revision"`
		Metadata *storage.DocumentMetadata `json:"metadata"`
	}
	if err := Decode(w, r, &in); err != nil {
		Fail(w, err)
		return
	}
	if in.Metadata == nil {
		Fail(w, errors.New("請提供文件資料 metadata"))
		return
	}
	if err := s.saveEdits(r.Context(), r.PathValue("id"), nil, metadataEdit{DocumentRevision: in.Revision, DocumentMetadata: in.Metadata}); err != nil {
		Fail(w, err)
		return
	}
	s.document(w, r)
}

// Enqueue deletion in every collection this chunk has ever been scheduled into.
func (s *Server) queueDelete(ctx context.Context, tx pgx.Tx, job, doc, id string) error {
	rows, e := tx.Query(ctx, `SELECT DISTINCT collection FROM indexing_tasks WHERE chunk_id=$1 AND kind='index' UNION SELECT $2::text`, id, s.Config.CollectionName())
	if e != nil {
		return e
	}
	collections := []string{}
	for rows.Next() {
		var c string
		if e = rows.Scan(&c); e != nil {
			rows.Close()
			return e
		}
		collections = append(collections, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, c := range collections {
		if e = storage.Task(ctx, tx, job, doc, id, "delete", c); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var job string
	e := s.Store.Transaction(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := storage.LockDocument(ctx, tx, id); e != nil {
			return e
		}
		chunks, e := storage.Chunks(ctx, tx, id)
		if e != nil {
			return e
		}
		job, e = storage.NewJob(ctx, tx, "delete")
		if e != nil {
			return e
		}
		for _, c := range chunks {
			if e = s.queueDelete(ctx, tx, job, id, c.ID); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, `DELETE FROM documents WHERE id=$1`, id)
		return e
	})
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 202, map[string]string{"job_id": job})
}
func (s *Server) rechunk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChunkSize int  `json:"chunk_size"`
		Revision  int  `json:"revision"`
		Confirm   bool `json:"confirm"`
	}
	if e := Decode(w, r, &in); e != nil {
		Fail(w, e)
		return
	}
	if !in.Confirm || in.ChunkSize < 32 || in.ChunkSize > 4096 {
		Fail(w, errors.New("請確認重切，大小需介於 32–4096"))
		return
	}
	id := r.PathValue("id")
	var job string
	e := s.Store.Transaction(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := storage.LockDocument(ctx, tx, id); e != nil {
			return e
		}
		var rev int
		var source string
		if e := tx.QueryRow(ctx, `SELECT revision,source_markdown FROM documents WHERE id=$1`, id).Scan(&rev, &source); e != nil {
			return e
		}
		if rev != in.Revision {
			return storage.ErrConflict
		}
		chunks, e := storage.Chunks(ctx, tx, id)
		if e != nil {
			return e
		}
		job, e = storage.NewJob(ctx, tx, "rechunk_cleanup")
		if e != nil {
			return e
		}
		for _, c := range chunks {
			if e = s.queueDelete(ctx, tx, job, id, c.ID); e != nil {
				return e
			}
		}
		if _, e = tx.Exec(ctx, `DELETE FROM document_chunks WHERE document_id=$1`, id); e != nil {
			return e
		}
		if e = storage.InsertParts(ctx, tx, id, chunker.Split(source, in.ChunkSize)); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE documents SET chunk_size=$2,status='draft',revision=revision+1,updated_at=now() WHERE id=$1`, id, in.ChunkSize)
		return e
	})
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 202, map[string]string{"job_id": job})
}
func (s *Server) commit(w http.ResponseWriter, r *http.Request) {
	job, e := s.Store.Enqueue(r.Context(), r.PathValue("id"), "", s.Config.ModelKey(), s.Config.CollectionName(), false)
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 202, map[string]string{"job_id": job})
}
func (s *Server) rebuild(w http.ResponseWriter, r *http.Request) {
	job, e := s.Store.Enqueue(r.Context(), "", "", s.Config.ModelKey(), s.Config.CollectionName(), true)
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 202, map[string]string{"job_id": job})
}
func (s *Server) reindex(w http.ResponseWriter, r *http.Request) {
	var doc string
	if e := s.Store.Pool.QueryRow(r.Context(), `SELECT document_id::text FROM document_chunks WHERE id=$1`, r.PathValue("id")).Scan(&doc); e != nil {
		Fail(w, e)
		return
	}
	job, e := s.Store.Enqueue(r.Context(), doc, r.PathValue("id"), s.Config.ModelKey(), s.Config.CollectionName(), true)
	if e != nil {
		Fail(w, e)
		return
	}
	JSON(w, 202, map[string]string{"job_id": job})
}
func validation(c storage.Chunk) error {
	if strings.TrimSpace(c.Raw) == "" || len(c.Raw) > 200000 {
		return fmt.Errorf("切片不可空白且不得超過 200 KB")
	}
	if len(c.Breadcrumbs) > 12 || len(c.Metadata.FAQ) > 10 {
		return fmt.Errorf("最多 12 層章節與 10 個 FAQ")
	}
	return nil
}
