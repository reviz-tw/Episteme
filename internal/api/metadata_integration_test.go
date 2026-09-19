package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reviz-tw/Episteme/internal/api"
	"github.com/reviz-tw/Episteme/internal/config"
	"github.com/reviz-tw/Episteme/internal/metadata"
	"github.com/reviz-tw/Episteme/internal/storage"
)

type extractFunc func(context.Context, string, string) (metadata.Extraction, error)

func (f extractFunc) Extract(c context.Context, s, m string) (metadata.Extraction, error) {
	return f(c, s, m)
}

func TestAutomaticMetadataLifecycle(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	server := httptest.NewServer((&api.Server{Store: store, Config: config.Config{Origin: "http://localhost:3000", MetadataEnabled: true, MetadataModel: "gemma2:2b"}}).Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	request := func(method, path string, body any, status int) []byte {
		t.Helper()
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, out)
		}
		return out
	}
	creds := map[string]string{"username": "tester", "password": "test-only-password-123"}
	request("POST", "/auth/setup", creds, 201)
	request("POST", "/auth/login", creds, 200)
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	part, _ := form.CreateFormFile("file", "metadata.md")
	_, _ = part.Write([]byte("# 公園調查\n\n都市公園鳥類調查。"))
	_ = form.Close()
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/documents/upload", &upload)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Origin", "http://localhost:3000")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct{ ID string }
	_ = json.NewDecoder(resp.Body).Decode(&uploaded)
	resp.Body.Close()
	if resp.StatusCode != 201 || uploaded.ID == "" {
		t.Fatal("upload failed")
	}
	id := uploaded.ID
	path := "/documents/" + id
	read := func() storage.Document {
		t.Helper()
		var d storage.Document
		if err := json.Unmarshal(request("GET", path, nil, 200), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	save := func(m storage.DocumentMetadata) {
		t.Helper()
		d := read()
		request("PATCH", path, map[string]any{"revision": d.Revision, "metadata": m}, 200)
	}
	d := read()
	if d.MetadataGeneration == nil || d.MetadataGeneration.Status != "queued" || d.Metadata.Title != "" {
		t.Fatal("upload did not queue a separate metadata job")
	}
	request("POST", path+"/metadata/extract", nil, 409)
	request("POST", path+"/commit", nil, 409)
	generated := metadata.Extraction{Metadata: storage.DocumentMetadata{Title: "公園調查", Author: "AI 建議", Tags: []string{"生態"}}, Sampled: true}
	worker := metadata.Worker{Store: store, Extractor: extractFunc(func(_ context.Context, source, model string) (metadata.Extraction, error) {
		if model != "gemma2:2b" || !strings.Contains(source, "公園調查") {
			t.Error("worker read wrong model/source")
		}
		return generated, nil
	})}
	step := func() {
		t.Helper()
		worked, err := worker.Step(ctx)
		if err != nil || !worked {
			t.Fatalf("worker: %v %v", worked, err)
		}
	}
	step()
	d = read()
	if d.Metadata.Title != "公園調查" || d.MetadataGeneration.Status != "completed" || !d.MetadataGeneration.Sampled || d.ReviewedCount != 0 {
		t.Fatal("metadata was not filled as unreviewed draft")
	}
	var published int
	_ = store.Pool.QueryRow(ctx, `SELECT count(*) FROM document_chunks WHERE document_id=$1 AND indexed_revision>0`, id).Scan(&published)
	if published != 0 {
		t.Fatal("metadata auto-published content")
	}
	request("POST", path+"/commit", nil, 409) // Chunk review is still mandatory.
	manual := storage.DocumentMetadata{Title: "人工標題", Tags: []string{"人工"}, Custom: map[string]string{"語言": "繁體中文"}}
	save(manual)
	request("POST", path+"/metadata/extract", nil, 202)
	step()
	d = read()
	if d.Metadata.Title != "人工標題" || d.Metadata.Author != "AI 建議" || d.Metadata.Tags[0] != "人工" || d.Metadata.Custom["語言"] != "繁體中文" {
		t.Fatal("refill overwrote manual fields")
	}
	request("POST", path+"/metadata/extract", nil, 202)
	worker.Extractor = extractFunc(func(context.Context, string, string) (metadata.Extraction, error) {
		save(storage.DocumentMetadata{})
		return generated, nil
	})
	step()
	d = read()
	if d.Metadata.Title != "" || d.Metadata.Author != "" || d.MetadataGeneration.Status != "skipped" {
		t.Fatal("in-flight generation overwrote user's clear")
	}
	request("POST", path+"/metadata/extract", nil, 202)
	worker.Extractor = extractFunc(func(context.Context, string, string) (metadata.Extraction, error) {
		return metadata.Extraction{}, errors.New("test unavailable")
	})
	for i := 0; i < 3; i++ {
		step()
		_, _ = store.Pool.Exec(ctx, `UPDATE metadata_jobs SET available_at=now() WHERE document_id=$1`, id)
	}
	if read().MetadataGeneration.Status != "failed" || read().MetadataGeneration.Attempts != 3 {
		t.Fatal("failure retries were not bounded")
	}
	request("POST", path+"/metadata/extract", nil, 202)
	_, _ = store.Pool.Exec(ctx, `UPDATE metadata_jobs SET status='running',run_token='00000000-0000-0000-0000-000000000001',available_at=now()-interval '1 second' WHERE document_id=$1`, id)
	worker.Extractor = extractFunc(func(context.Context, string, string) (metadata.Extraction, error) { return generated, nil })
	step()
	if read().MetadataGeneration.Status != "completed" {
		t.Fatal("expired worker lease did not recover")
	}
	large := storage.DocumentMetadata{Custom: map[string]string{}}
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		large.Custom[key] = strings.Repeat("x", 2000)
	}
	save(large)
	request("POST", path+"/metadata/extract", nil, 202)
	worker.Extractor = extractFunc(func(context.Context, string, string) (metadata.Extraction, error) {
		return metadata.Extraction{Metadata: storage.DocumentMetadata{Tags: []string{strings.Repeat("a", 80), strings.Repeat("b", 80), strings.Repeat("c", 80), strings.Repeat("d", 80), strings.Repeat("e", 80), strings.Repeat("f", 80)}}}, nil
	})
	step()
	if read().MetadataGeneration.Status != "failed" || len(read().Metadata.Tags) != 0 {
		t.Fatal("invalid merge did not preserve draft and terminate the job")
	}
	save(storage.DocumentMetadata{})
	// A superseded worker may not write its result after a newer claim.
	request("POST", path+"/metadata/extract", nil, 202)
	worker.Extractor = extractFunc(func(context.Context, string, string) (metadata.Extraction, error) {
		_, e := store.Pool.Exec(ctx, `UPDATE metadata_jobs SET status='queued',run_token=NULL,available_at=now() WHERE document_id=$1`, id)
		return generated, e
	})
	step()
	if read().MetadataGeneration.Status != "queued" {
		t.Fatal("stale worker overwrote a newer claim")
	}
	// Deletion cancels the job through the FK; completion cannot resurrect it.
	worker.Extractor = extractFunc(func(context.Context, string, string) (metadata.Extraction, error) {
		request("DELETE", path, nil, 202)
		return generated, nil
	})
	step()
	request("GET", path, nil, 404)
	var jobs int
	_ = store.Pool.QueryRow(ctx, `SELECT count(*) FROM metadata_jobs WHERE document_id=$1`, id).Scan(&jobs)
	if jobs != 0 {
		t.Fatal("deleted document retained metadata job")
	}
}
