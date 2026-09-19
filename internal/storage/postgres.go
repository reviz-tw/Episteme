package storage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/reviz-tw/Episteme/internal/chunker"
	dbschema "github.com/reviz-tw/Episteme/sql"
)

var ErrConflict = errors.New("內容已由其他操作更新，請重新載入")
var ErrReview = errors.New("請先確認所有未排除的切片")

type Store struct{ Pool *pgxpool.Pool }
type Document struct {
	ID                 string              `json:"id"`
	Filename           string              `json:"filename"`
	MimeType           string              `json:"mime_type"`
	Source             string              `json:"source_markdown"`
	Status             string              `json:"status"`
	ChunkSize          int                 `json:"chunk_size"`
	Revision           int                 `json:"revision"`
	TotalTokens        int                 `json:"total_tokens"`
	ChunkCount         int                 `json:"chunk_count"`
	ReviewedCount      int                 `json:"reviewed_count"`
	CreatedAt          string              `json:"created_at"`
	UpdatedAt          string              `json:"updated_at"`
	Metadata           DocumentMetadata    `json:"metadata"`
	MetadataGeneration *MetadataGeneration `json:"metadata_generation"`
}
type Chunk struct {
	ID          string   `json:"id"`
	DocumentID  string   `json:"document_id"`
	Index       int      `json:"chunk_index"`
	Content     string   `json:"content"`
	Raw         string   `json:"raw_markdown"`
	Breadcrumbs []string `json:"breadcrumbs"`
	Tokens      int      `json:"token_count"`
	Reviewed    bool     `json:"is_reviewed"`
	Excluded    bool     `json:"is_excluded"`
	Dirty       bool     `json:"is_dirty"`
	Metadata    struct {
		FAQ []string `json:"faq"`
	} `json:"metadata"`
	Start                   int              `json:"source_start"`
	End                     int              `json:"source_end"`
	Revision                int              `json:"revision"`
	IndexedRevision         int              `json:"indexed_revision"`
	IndexedContent          string           `json:"indexed_content"`
	IndexedModel            string           `json:"indexed_model"`
	IndexedBreadcrumbs      []string         `json:"indexed_breadcrumbs"`
	IndexedDocumentMetadata DocumentMetadata `json:"indexed_document_metadata"`
	DocumentMetadata        DocumentMetadata `json:"-"`
}
type Job struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Error     string `json:"error"`
	CreatedAt string `json:"created_at"`
}

func Open(ctx context.Context, url string) (*Store, error) {
	p, e := pgxpool.New(ctx, url)
	if e != nil {
		return nil, e
	}
	s := &Store{p}
	if _, e = p.Exec(ctx, dbschema.Schema); e != nil {
		p.Close()
		return nil, e
	}
	return s, nil
}
func JSONRow(row pgx.Row, v any) error {
	var b []byte
	if e := row.Scan(&b); e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}

const documentJSON = `SELECT jsonb_build_object('id',d.id,'filename',d.filename,'mime_type',d.mime_type,'status',
CASE WHEN d.status='indexing' AND NOT EXISTS(SELECT 1 FROM indexing_tasks t JOIN indexing_jobs j ON j.id=t.job_id WHERE t.document_id=d.id AND NOT t.done AND j.status IN ('queued','running','paused')) THEN CASE WHEN bool_or(c.indexed_revision>0) THEN 'stale' ELSE 'draft' END ELSE d.status END,
'chunk_size',d.chunk_size,'revision',d.revision,'created_at',d.created_at,'updated_at',d.updated_at,'metadata',d.metadata,
'metadata_generation',(SELECT jsonb_build_object('status',j.status,'model',j.model,'attempts',j.attempts,'error',j.error,'sampled',j.sampled,'updated_at',j.updated_at) FROM metadata_jobs j WHERE j.document_id=d.id),
'chunk_count',count(c.id),'total_tokens',coalesce(sum(c.token_count),0),'reviewed_count',count(c.id) FILTER(WHERE c.is_reviewed OR c.is_excluded)) FROM documents d LEFT JOIN document_chunks c ON c.document_id=d.id `

func (s *Store) Documents(ctx context.Context) ([]Document, error) {
	rows, e := s.Pool.Query(ctx, documentJSON+` GROUP BY d.id ORDER BY d.updated_at DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		var d Document
		if e = JSONRow(rows, &d); e != nil {
			return nil, e
		}
		d.Source = ""
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) Document(ctx context.Context, id string) (Document, error) {
	var d Document
	e := JSONRow(s.Pool.QueryRow(ctx, documentJSON+` WHERE d.id=$1 GROUP BY d.id`, id), &d)
	if e == nil {
		e = s.Pool.QueryRow(ctx, `SELECT source_markdown FROM documents WHERE id=$1`, id).Scan(&d.Source)
	}
	return d, e
}

type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func Chunks(ctx context.Context, q Querier, id string) ([]Chunk, error) {
	rows, e := q.Query(ctx, `SELECT to_jsonb(c) FROM document_chunks c WHERE document_id=$1 ORDER BY chunk_index`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Chunk{}
	for rows.Next() {
		var c Chunk
		if e = JSONRow(rows, &c); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) Jobs(ctx context.Context) ([]Job, error) {
	rows, e := s.Pool.Query(ctx, `SELECT to_jsonb(j) FROM indexing_jobs j ORDER BY created_at DESC LIMIT 50`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		if e = JSONRow(rows, &j); e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (s *Store) Transaction(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func LockDocument(ctx context.Context, tx pgx.Tx, id string) error {
	var x string
	return tx.QueryRow(ctx, `SELECT id::text FROM documents WHERE id=$1 FOR UPDATE`, id).Scan(&x)
}
func InsertParts(ctx context.Context, tx pgx.Tx, id string, parts []chunker.Part) error {
	for i, p := range parts {
		c := Chunk{ID: uuid.NewString(), DocumentID: id, Index: i, Raw: p.Raw, Breadcrumbs: p.Breadcrumbs, Start: p.Start, End: p.End, Revision: 1, Dirty: true}
		if e := InsertChunk(ctx, tx, c); e != nil {
			return e
		}
	}
	return nil
}
func InsertChunk(ctx context.Context, tx pgx.Tx, c Chunk) error {
	content := chunker.Content(c.Raw, c.Breadcrumbs, c.Metadata.FAQ)
	if c.Breadcrumbs == nil {
		c.Breadcrumbs = []string{}
	}
	_, e := tx.Exec(ctx, `INSERT INTO document_chunks(id,document_id,chunk_index,content,raw_markdown,breadcrumbs,token_count,metadata,source_start,source_end) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.ID, c.DocumentID, c.Index, content, c.Raw, c.Breadcrumbs, chunker.Tokens(content), c.Metadata, c.Start, c.End)
	return e
}
func SaveChunk(ctx context.Context, tx pgx.Tx, c Chunk) error {
	content := chunker.Content(c.Raw, c.Breadcrumbs, c.Metadata.FAQ)
	_, e := tx.Exec(ctx, `UPDATE document_chunks SET raw_markdown=$2,content=$3,breadcrumbs=$4,token_count=$5,metadata=$6,is_reviewed=$7,is_excluded=$8,is_dirty=is_dirty OR content<>$3 OR is_excluded<>$8,revision=revision+1,updated_at=now() WHERE id=$1`, c.ID, c.Raw, content, c.Breadcrumbs, chunker.Tokens(content), c.Metadata, c.Reviewed, c.Excluded)
	return e
}
func RefreshDocument(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `UPDATE documents d SET revision=revision+1,updated_at=now(),status=CASE
 WHEN EXISTS(SELECT 1 FROM indexing_tasks t JOIN indexing_jobs j ON j.id=t.job_id WHERE t.document_id=d.id AND t.kind='index' AND NOT t.done AND j.status IN ('queued','running','paused')) THEN 'indexing'
 WHEN EXISTS(SELECT 1 FROM document_chunks c WHERE c.document_id=d.id AND c.is_dirty AND NOT c.is_excluded) THEN CASE WHEN EXISTS(SELECT 1 FROM document_chunks c WHERE c.document_id=d.id AND c.indexed_revision>0) THEN 'stale' ELSE 'draft' END
 ELSE 'indexed' END WHERE id=$1`, id)
	return e
}
func NewJob(ctx context.Context, tx pgx.Tx, kind string) (string, error) {
	id := uuid.NewString()
	_, e := tx.Exec(ctx, `INSERT INTO indexing_jobs(id,kind) VALUES($1,$2)`, id, kind)
	return id, e
}
func Task(ctx context.Context, tx pgx.Tx, job, doc, chunk, kind, collection string) error {
	if _, e := tx.Exec(ctx, `INSERT INTO indexing_tasks(job_id,document_id,chunk_id,kind,collection,document_metadata)VALUES($1,$2,$3,$4,$5,coalesce((SELECT metadata FROM documents WHERE id=$2),'{}'::jsonb))`, job, doc, chunk, kind, collection); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `UPDATE indexing_jobs SET total=total+1 WHERE id=$1`, job)
	return e
}
func (s *Store) Enqueue(ctx context.Context, docID, chunkID, model, collection string, force bool) (string, error) {
	var job string
	e := s.Transaction(ctx, func(tx pgx.Tx) error {
		var e error
		kind := "commit"
		if force {
			kind = "rebuild"
		}
		job, e = NewJob(ctx, tx, kind)
		if e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id::text FROM documents WHERE ($1='' OR id::text=$1) ORDER BY id FOR UPDATE`, docID)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if docID != "" && len(ids) == 0 {
			return pgx.ErrNoRows
		}
		for _, id := range ids {
			var pending bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM metadata_jobs WHERE document_id=$1 AND status IN ('queued','running'))`, id).Scan(&pending); e != nil {
				return e
			}
			if pending {
				return ErrMetadataPending
			}
			chunks, e := Chunks(ctx, tx, id)
			if e != nil {
				return e
			}
			found := chunkID == ""
			for _, c := range chunks {
				if chunkID != "" && c.ID != chunkID {
					continue
				}
				found = true
				if !c.Excluded && !c.Reviewed {
					return ErrReview
				}
				if c.Excluded {
					continue
				}
				if force || c.Dirty || c.IndexedModel != model {
					if e = Task(ctx, tx, job, id, c.ID, "index", collection); e != nil {
						return e
					}
				}
			}
			if !found {
				return pgx.ErrNoRows
			}
			if e = RefreshDocument(ctx, tx, id); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, `UPDATE indexing_jobs SET status='completed' WHERE id=$1 AND total=0`, job)
		return e
	})
	return job, e
}
