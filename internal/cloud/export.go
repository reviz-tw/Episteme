// Package cloud prepares an offline JSONL export. It never uploads documents.
package cloud

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/reviz-tw/Episteme/internal/storage"
	"github.com/reviz-tw/Episteme/internal/storage/db"
	"io"
)

// Export emits published chunks as Vertex AI Search structured documents.
// The destination data store must define document_id, content, breadcrumbs,
// revision and document_metadata. Cloud credentials are not read or required.
func Export(ctx context.Context, store *storage.Store, out io.Writer) error {
	chunks, e := db.New(store.Pool).PublishedChunks(ctx)
	if e != nil {
		return e
	}
	enc := json.NewEncoder(out)
	for _, c := range chunks {
		var metadata storage.DocumentMetadata
		if e = json.Unmarshal(c.IndexedDocumentMetadata, &metadata); e != nil {
			return e
		}
		var crumbs []string
		if e = json.Unmarshal(c.IndexedBreadcrumbs, &crumbs); e != nil {
			return e
		}
		data, e := json.Marshal(map[string]any{"document_id": uuid.UUID(c.DocumentID.Bytes).String(), "content": c.IndexedContent, "breadcrumbs": crumbs, "revision": c.IndexedRevision, "document_metadata": metadata})
		if e != nil {
			return e
		}
		if e = enc.Encode(map[string]any{"id": uuid.UUID(c.ID.Bytes).String(), "jsonData": string(data)}); e != nil {
			return e
		}
	}
	return nil
}
