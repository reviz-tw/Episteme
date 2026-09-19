package storage

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// DocumentMetadata describes provenance; chunk Metadata remains reserved for FAQ.
type DocumentMetadata struct {
	Title       string            `json:"title,omitempty"`
	Source      string            `json:"source,omitempty"`
	SourceURL   string            `json:"source_url,omitempty"`
	Author      string            `json:"author,omitempty"`
	PublishedAt string            `json:"published_at,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Custom      map[string]string `json:"custom,omitempty"`
}

func NormalizeMetadata(m DocumentMetadata) (DocumentMetadata, error) {
	valid := func(s string, max int) bool {
		return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= max
	}
	m.Title, m.Source, m.Author = strings.TrimSpace(m.Title), strings.TrimSpace(m.Source), strings.TrimSpace(m.Author)
	m.SourceURL, m.PublishedAt = strings.TrimSpace(m.SourceURL), strings.TrimSpace(m.PublishedAt)
	if !valid(m.Title, 300) || !valid(m.Source, 200) || !valid(m.Author, 200) || !valid(m.SourceURL, 2048) {
		return m, errors.New("文件資料過長或含無效字元：標題最多 300 字，來源／作者 200 字，網址 2048 字")
	}
	if m.SourceURL != "" {
		u, err := url.Parse(m.SourceURL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return m, errors.New("來源網址必須是完整的 HTTP 或 HTTPS 網址")
		}
	}
	if m.PublishedAt != "" {
		if _, err := time.Parse("2006-01-02", m.PublishedAt); err != nil {
			return m, errors.New("發布日期請使用 YYYY-MM-DD 格式")
		}
	}
	tags, seen := []string{}, map[string]bool{}
	for _, tag := range m.Tags {
		tag = strings.TrimSpace(tag)
		if !valid(tag, 80) {
			return m, errors.New("每個標籤最多 80 字且不可含無效字元")
		}
		if tag != "" && !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	if len(tags) > 30 {
		return m, errors.New("每份文件最多 30 個標籤")
	}
	m.Tags = tags
	custom := map[string]string{}
	for key, value := range m.Custom {
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "" || !valid(key, 80) || !valid(value, 2000) {
			return m, errors.New("自訂欄位名稱需為 1–80 字，值最多 2000 字且不可含無效字元")
		}
		if _, exists := custom[key]; exists {
			return m, errors.New("自訂欄位名稱不可重複")
		}
		custom[key] = value
	}
	if len(custom) > 30 {
		return m, errors.New("每份文件最多 30 個自訂欄位")
	}
	m.Custom = custom
	data, _ := json.Marshal(m)
	if len(data) > 16<<10 {
		return m, errors.New("文件資料合計不可超過 16 KB")
	}
	return m, nil
}

// Payload uses native JSON values so nested fields can be represented by Qdrant.
func (m DocumentMetadata) Payload() map[string]any {
	data, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

// Caller holds the document lock and refreshes its revision in the same transaction.
func SaveDocumentMetadata(ctx context.Context, tx pgx.Tx, id string, revision int, metadata DocumentMetadata) error {
	var current int
	if err := tx.QueryRow(ctx, `SELECT revision FROM documents WHERE id=$1`, id).Scan(&current); err != nil {
		return err
	}
	if current != revision {
		return ErrConflict
	}
	// Even saving an unchanged/empty form means the user has taken ownership.
	if _, err := tx.Exec(ctx, `UPDATE documents SET metadata_revision=metadata_revision+1 WHERE id=$1`, id); err != nil {
		return err
	}
	return ApplyDocumentMetadata(ctx, tx, id, metadata)
}

// ApplyDocumentMetadata updates a draft under the document lock, without claiming
// a manual edit. Background extraction calls this only after checking its base revision.
func ApplyDocumentMetadata(ctx context.Context, tx pgx.Tx, id string, metadata DocumentMetadata) error {
	result, err := tx.Exec(ctx, `UPDATE documents SET metadata=$2 WHERE id=$1 AND metadata IS DISTINCT FROM $2::jsonb`, id, metadata)
	if err != nil || result.RowsAffected() == 0 {
		return err
	}
	// Content approval and exclusions stay intact. Metadata still needs a new commit.
	_, err = tx.Exec(ctx, `UPDATE document_chunks SET is_dirty=true,revision=revision+1,updated_at=now() WHERE document_id=$1`, id)
	return err
}
