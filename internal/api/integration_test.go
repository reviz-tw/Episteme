package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/reviz-tw/Episteme/internal/api"
	"github.com/reviz-tw/Episteme/internal/cloud"
	"github.com/reviz-tw/Episteme/internal/config"
	"github.com/reviz-tw/Episteme/internal/indexer"
	"github.com/reviz-tw/Episteme/internal/inference"
	"github.com/reviz-tw/Episteme/internal/retrieval"
	"github.com/reviz-tw/Episteme/internal/storage"
	dbschema "github.com/reviz-tw/Episteme/sql"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// These contract doubles are confined to tests. PostgreSQL, HTTP routing,
// sessions, TEI clients, queue transactions and retrieval filtering run for real.
type vectors struct {
	mu      sync.Mutex
	points  map[string]storage.Chunk
	fail    bool
	upserts int
}

func (v *vectors) Ensure(context.Context) error { return nil }
func (v *vectors) Upsert(_ context.Context, c storage.Chunk, _ []float32, _ []uint32, _ []float32) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.fail {
		return errors.New("simulated vector service failure")
	}
	v.points[c.ID] = c
	v.upserts++
	return nil
}
func (v *vectors) Delete(_ context.Context, id, _ string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.fail {
		return errors.New("simulated delete failure")
	}
	delete(v.points, id)
	return nil
}
func (v *vectors) Search(_ context.Context, _ string, _ []float32, _ []uint32, _ []float32, _ int) ([]storage.Hit, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := []storage.Hit{}
	for _, c := range v.points {
		out = append(out, storage.Hit{ID: c.ID, Revision: c.Revision, Score: 1})
	}
	return out, nil
}
func (v *vectors) Health(context.Context) (uint64, error) { return uint64(len(v.points)), nil }
func testStore(t *testing.T) *storage.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	schema := "episteme_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, `CREATE SCHEMA `+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, dbschema.Schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})
	return &storage.Store{Pool: pool}
}
func TestDocumentLifecycle(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	v := &vectors{points: map[string]storage.Chunk{}}
	teiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			api.JSON(w, 200, map[string]string{"model_id": "test-model"})
		case "/embed":
			api.JSON(w, 200, [][]float32{{1, 0, 0}})
		case "/rerank":
			var input struct {
				Texts []string `json:"texts"`
			}
			_ = json.NewDecoder(r.Body).Decode(&input)
			out := []map[string]any{}
			for i := range input.Texts {
				out = append(out, map[string]any{"index": i, "score": float64(i+1) / 10})
			}
			api.JSON(w, 200, out)
		case "/health":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer teiServer.Close()
	model := inference.New(teiServer.URL, teiServer.URL, "test-model", 3)
	cfg := config.Config{Origin: "http://localhost:3000", Model: "test-model", Dimension: 3, Collection: "test", EmbedURL: teiServer.URL, RerankURL: teiServer.URL}
	search := &retrieval.Service{Store: store, Vectors: v, Model: model, ModelKey: cfg.ModelKey()}
	server := httptest.NewServer((&api.Server{Store: store, Config: cfg, TEI: model, Vectors: v, Search: search}).Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	worker := &indexer.Worker{Store: store, Vectors: v, Model: model, ModelKey: cfg.ModelKey(), Collection: cfg.CollectionName()}
	request := func(method, path string, body any, status int) []byte {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", cfg.Origin)
		resp, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("%s %s status %d expected %d: %s", method, path, resp.StatusCode, status, data)
		}
		return data
	}
	drain := func() {
		t.Helper()
		for i := 0; i < 30; i++ {
			worked, e := worker.Step(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if !worked {
				return
			}
		}
		t.Fatal("worker did not drain")
	}
	request("GET", "/documents", nil, 401)
	creds := map[string]string{"username": "tester", "password": "test-only-password-123"}
	request("POST", "/auth/setup", creds, 201)
	request("POST", "/auth/setup", creds, 400)
	request("POST", "/auth/login", creds, 200)
	t.Run("reject foreign origin", func(t *testing.T) {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/logout", strings.NewReader("{}"))
		req.Header.Set("Origin", "https://evil.example")
		resp, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatal(resp.StatusCode)
		}
	})
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	f, _ := form.CreateFormFile("file", "knowledge.md")
	_, _ = f.Write([]byte("# 知識手冊\n\n可信任的原始說明。\n\n## 使用方法\n\n只有已審核的內容會進入檢索。\n"))
	form.Close()
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/documents/upload", &upload)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != 201 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: %s", data)
	}
	_ = json.NewDecoder(resp.Body).Decode(&uploaded)
	resp.Body.Close()
	doc := uploaded.ID
	getChunks := func() []storage.Chunk {
		t.Helper()
		var chunks []storage.Chunk
		if e := json.Unmarshal(request("GET", "/documents/"+doc+"/chunks", nil, 200), &chunks); e != nil {
			t.Fatal(e)
		}
		return chunks
	}
	chunks := getChunks()
	if len(chunks) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(chunks))
	}
	request("POST", "/documents/"+doc+"/commit", nil, 409)
	first := chunks[0]
	patch := map[string]any{"revision": first.Revision, "raw_markdown": first.Raw + "\n草稿修訂。", "is_reviewed": true, "faq": []string{"什麼是可信任的內容？"}}
	request("PATCH", "/chunks/"+first.ID, patch, 200)
	request("PATCH", "/chunks/"+first.ID, patch, 409)
	chunks = getChunks()
	if !strings.Contains(chunks[0].Raw, "草稿修訂") {
		t.Fatal("draft not persisted")
	}
	if len(v.points) != 0 {
		t.Fatal("draft unexpectedly indexed")
	}
	request("PATCH", "/chunks/"+chunks[1].ID, map[string]any{"revision": chunks[1].Revision, "is_reviewed": true}, 200)
	request("POST", "/documents/"+doc+"/commit", nil, 202)
	drain()
	if len(v.points) != 2 {
		t.Fatal("index missing", len(v.points))
	}
	var results []retrieval.Result
	_ = json.Unmarshal(request("POST", "/retrieval/search", retrieval.Input{Query: "可信任", Alpha: .5, Rerank: true}, 200), &results)
	if len(results) != 2 || results[0].RerankScore == nil {
		t.Fatal("no indexed recall/rerank", results)
	}
	chunks = getChunks()
	old := chunks[0].IndexedContent
	request("PATCH", "/chunks/"+first.ID, map[string]any{"revision": chunks[0].Revision, "raw_markdown": "更新後的核准知識。", "is_reviewed": true}, 200)
	_ = json.Unmarshal(request("POST", "/retrieval/search", retrieval.Input{Query: "知識", Alpha: 0, IncludeLowRelevance: true}, 200), &results)
	found := false
	for _, r := range results {
		if r.ChunkID == first.ID {
			found = true
			if r.Content != old {
				t.Fatal("unpublished draft leaked")
			}
		}
	}
	if !found {
		t.Fatal("published copy missing")
	}
	before := v.upserts
	request("POST", "/chunks/"+first.ID+"/reindex", nil, 202)
	drain()
	if v.upserts != before+1 {
		t.Fatal("partial update reindexed unrelated chunk")
	}
	chunks = getChunks()
	request("PATCH", "/chunks/"+first.ID, map[string]any{"revision": chunks[0].Revision, "is_excluded": true}, 200)
	_ = json.Unmarshal(request("POST", "/retrieval/search", retrieval.Input{Query: "知識", Alpha: 0, IncludeLowRelevance: true}, 200), &results)
	if len(results) != 1 {
		t.Fatal("excluded point leaked before cleanup")
	}
	drain()
	if len(v.points) != 1 {
		t.Fatal("excluded point not deleted")
	}
	chunks = getChunks()
	request("PATCH", "/chunks/"+first.ID, map[string]any{"revision": chunks[0].Revision, "is_excluded": false, "is_reviewed": true}, 200)
	request("POST", "/chunks/"+first.ID+"/reindex", nil, 202)
	v.fail = true
	drain()
	jobs, e := store.Jobs(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if jobs[0].Error == "" {
		t.Fatal("failure not durable")
	}
	v.fail = false
	request("POST", "/indexing/jobs/"+jobs[0].ID, map[string]string{"action": "pause"}, 200)
	drain()
	request("POST", "/indexing/jobs/"+jobs[0].ID, map[string]string{"action": "resume"}, 200)
	drain()
	if len(v.points) != 2 {
		t.Fatal("retry failed")
	}
	chunks = getChunks()
	request("POST", "/chunks/"+first.ID+"/split", map[string]any{"revision": chunks[0].Revision, "position": 3}, 200)
	chunks = getChunks()
	if len(chunks) != 3 {
		t.Fatal("split")
	}
	request("POST", "/chunks/"+first.ID+"/merge", map[string]any{"revision": chunks[0].Revision, "next_revision": chunks[1].Revision}, 200)
	chunks = getChunks()
	if len(chunks) != 2 {
		t.Fatal("merge")
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Fatal("non-contiguous chunk order")
		}
	}
	var d storage.Document
	_ = json.Unmarshal(request("GET", "/documents/"+doc, nil, 200), &d)
	request("POST", "/documents/"+doc+"/rechunk", map[string]any{"revision": d.Revision, "chunk_size": 1024, "confirm": true}, 202)
	chunks = getChunks()
	for _, c := range chunks {
		if c.Reviewed || c.IndexedRevision != 0 {
			t.Fatal("rechunk preserved review/publication")
		}
	}
	drain()
	if len(v.points) != 0 {
		t.Fatal("old vectors survived rechunk")
	}
	request("DELETE", "/documents/"+doc, nil, 202)
	drain()
	request("GET", "/documents/"+doc, nil, 404)
	// JSON follows the same review/publication/rechunk pipeline and retains its bytes.
	uploadJSON := func(name, content string, status int) string {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, _ := form.CreateFormFile("file", name)
		_, _ = io.WriteString(file, content)
		_ = form.Close()
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/documents/upload", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("JSON upload %d, expected %d: %s", resp.StatusCode, status, data)
		}
		var result struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(data, &result)
		return result.ID
	}
	uploadJSON("broken.json", `{"items": [}`, 400)
	uploadJSON("duplicate.json", `{"a":1,"a":2}`, 400)
	uploadJSON("multiple.json", "{}\n{}", 400)
	var invalidCount int
	if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM documents`).Scan(&invalidCount); err != nil || invalidCount != 0 {
		t.Fatalf("invalid JSON persisted a document: count=%d error=%v", invalidCount, err)
	}
	jsonOriginal := "\ufeff[\n  {\"title\":\"中文 JSON 第一筆\",\"id\":9007199254740993},\n  {\"title\":\"中文 JSON 第二筆\",\"active\":true}\n]\n"
	doc = uploadJSON("records.JSON", jsonOriginal, 201)
	if err := json.Unmarshal(request("GET", "/documents/"+doc, nil, 200), &d); err != nil || d.MimeType != "application/json" {
		t.Fatalf("JSON document MIME: %+v, %v", d, err)
	}
	original, err := client.Get(server.URL + "/api/v1/documents/" + doc + "/original")
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, _ := io.ReadAll(original.Body)
	original.Body.Close()
	if original.StatusCode != 200 || original.Header.Get("Content-Type") != "application/json" || string(originalBytes) != jsonOriginal {
		t.Fatal("original JSON bytes or content type changed")
	}
	chunks = getChunks()
	if len(chunks) != 2 || !strings.Contains(chunks[0].Raw, "9007199254740993") {
		t.Fatalf("JSON records not preserved: %+v", chunks)
	}
	for _, chunk := range chunks {
		request("PATCH", "/chunks/"+chunk.ID, map[string]any{"revision": chunk.Revision, "is_reviewed": true}, 200)
	}
	request("POST", "/documents/"+doc+"/commit", nil, 202)
	drain()
	_ = json.Unmarshal(request("POST", "/retrieval/search", retrieval.Input{Query: "JSON", Alpha: .5, Rerank: true}, 200), &results)
	if len(results) != 2 || !strings.Contains(results[0].Content, "JSON") {
		t.Fatalf("published JSON not searchable: %+v", results)
	}
	// Quality filtering is optional; publication and exclusion checks are not.
	minimum := 1.0
	searchInput := retrieval.Input{Query: "完全無關的查詢", Alpha: .5, Rerank: true, MinScore: &minimum}
	_ = json.Unmarshal(request("POST", "/retrieval/search", searchInput, 200), &results)
	if len(results) != 0 {
		t.Fatal("weak results were padded into the response")
	}
	searchInput.IncludeLowRelevance = true
	_ = json.Unmarshal(request("POST", "/retrieval/search", searchInput, 200), &results)
	if len(results) != 2 || results[0].MatchType != "candidate" {
		t.Fatal("debug candidates missing or mislabeled")
	}
	_ = json.Unmarshal(request("POST", "/retrieval/search", retrieval.Input{Query: "JSON", Alpha: .5, Rerank: true, Limit: 1}, 200), &results)
	if len(results) != 1 || results[0].RerankScore == nil || *results[0].RerankScore != .2 {
		t.Fatal("limit was applied before reranking the candidate pool")
	}
	request("POST", "/retrieval/search", map[string]any{"query": "JSON", "alpha": .5, "min_score": 1.1}, 400)

	getDocument := func() storage.Document {
		t.Helper()
		var document storage.Document
		if err := json.Unmarshal(request("GET", "/documents/"+doc, nil, 200), &document); err != nil {
			t.Fatal(err)
		}
		return document
	}
	setMetadata := func(metadata storage.DocumentMetadata) {
		t.Helper()
		current := getDocument()
		request("PATCH", "/documents/"+doc, map[string]any{"revision": current.Revision, "metadata": metadata}, 200)
	}
	assertPublishedMetadata := func(title string) {
		t.Helper()
		var results []retrieval.Result
		if err := json.Unmarshal(request("POST", "/retrieval/search", retrieval.Input{Query: "JSON", Alpha: .5, Rerank: true}, 200), &results); err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("metadata publication lost results: %+v", results)
		}
		for _, result := range results {
			if result.DocumentMetadata.Title != title {
				t.Fatalf("unexpected published metadata: %+v", result.DocumentMetadata)
			}
			if title == "" && len(result.DocumentMetadata.Payload()) != 0 {
				t.Fatalf("cleared metadata retained fields: %+v", result.DocumentMetadata)
			}
		}
	}
	metadataA := storage.DocumentMetadata{Title: "核可版本 A", Source: "測試來源", SourceURL: "https://example.org/source", Author: "測試作者", PublishedAt: "2026-09-19", Tags: []string{"查核", "JSON"}, Custom: map[string]string{"語言": "繁體中文"}}
	d = getDocument()
	beforeChunks := getChunks()
	setMetadata(metadataA)
	request("PATCH", "/documents/"+doc, map[string]any{"revision": d.Revision, "metadata": metadataA}, 409)
	if getDocument().Metadata.Custom["語言"] != "繁體中文" {
		t.Fatal("metadata draft not saved")
	}
	chunks = getChunks()
	for i, c := range chunks {
		if !c.Reviewed || !c.Dirty || c.Raw != beforeChunks[i].Raw {
			t.Fatal("metadata edit changed content approval or failed to mark indexing stale")
		}
	}
	assertPublishedMetadata("")
	request("POST", "/documents/"+doc+"/commit", nil, 202)
	metadataB := metadataA
	metadataB.Title = "未核可版本 B"
	setMetadata(metadataB) // A queued job must not publish a later metadata draft.
	drain()
	assertPublishedMetadata("核可版本 A")
	for _, c := range getChunks() {
		if !c.Dirty {
			t.Fatal("old job incorrectly cleared pending metadata changes")
		}
	}
	for _, point := range v.points {
		if point.DocumentMetadata.Title != "核可版本 A" || strings.Contains(point.Content, "核可版本 A") {
			t.Fatal("metadata snapshot or content boundary violated")
		}
	}
	var exported bytes.Buffer
	if err := cloud.Export(ctx, store, &exported); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported.String(), "核可版本 A") || strings.Contains(exported.String(), "未核可版本 B") {
		t.Fatal("export leaked metadata draft")
	}
	request("POST", "/documents/"+doc+"/commit", nil, 202)
	drain()
	assertPublishedMetadata("未核可版本 B")
	for _, c := range getChunks() {
		if c.Dirty {
			t.Fatal("metadata publication left chunks stale")
		}
	}

	// Chunk edits and metadata save atomically, including conflict rollback.
	d = getDocument()
	chunks = getChunks()
	combined := map[string]any{"document_revision": d.Revision - 1, "document_metadata": metadataA, "chunks": []map[string]any{{"id": chunks[0].ID, "revision": chunks[0].Revision, "raw_markdown": "JSON 合併儲存測試。", "is_reviewed": true}}}
	request("PATCH", "/documents/"+doc+"/chunks", combined, 409)
	if getChunks()[0].Raw != chunks[0].Raw {
		t.Fatal("chunk change escaped a metadata conflict rollback")
	}
	combined["document_revision"] = d.Revision
	request("PATCH", "/documents/"+doc+"/chunks", combined, 200)
	if getDocument().Metadata.Title != metadataA.Title || getChunks()[0].Raw != "JSON 合併儲存測試。" {
		t.Fatal("combined draft not saved")
	}
	assertPublishedMetadata("未核可版本 B")
	setMetadata(storage.DocumentMetadata{})
	if getDocument().Metadata.Title != "" || len(getDocument().Metadata.Tags) != 0 {
		t.Fatal("metadata could not be cleared")
	}
	request("POST", "/documents/"+doc+"/commit", nil, 202)
	drain()
	assertPublishedMetadata("")
	setMetadata(metadataA)
	_ = json.Unmarshal(request("GET", "/documents/"+doc, nil, 200), &d)
	request("POST", "/documents/"+doc+"/rechunk", map[string]any{"revision": d.Revision, "chunk_size": 1024, "confirm": true}, 202)
	if len(getChunks()) != 2 {
		t.Fatal("JSON record boundaries were lost during rechunk")
	}
	if getDocument().Metadata.Title != metadataA.Title {
		t.Fatal("rechunk lost document metadata")
	}
	drain()
	request("DELETE", "/documents/"+doc, nil, 202)
	drain()
	request("POST", "/auth/logout", nil, 200)
	request("GET", "/documents", nil, 401)
	t.Log(fmt.Sprintf("Verified document %s: upload, draft, conflict, commit, published search, partial update, exclusion, retry, pause/resume, Unicode split/merge, rechunk, delete", doc))
}
