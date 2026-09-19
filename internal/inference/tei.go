package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

type TEI struct {
	EmbedURL, RerankURL, Model string
	Dimension                  int
	HTTP                       *http.Client
}

func New(embed, rerank, model string, dim int) *TEI {
	return &TEI{embed, rerank, model, dim, &http.Client{Timeout: 90 * time.Second}}
}
func (t *TEI) request(ctx context.Context, url string, input, output any) error {
	var body io.Reader
	method := "GET"
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
		method = "POST"
	}
	req, e := http.NewRequestWithContext(ctx, method, url, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	r, e := t.HTTP.Do(req)
	if e != nil {
		return fmt.Errorf("TEI unavailable: %w", e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("TEI returned HTTP %d", r.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(output)
}
func (t *TEI) Embed(ctx context.Context, text string) ([]float32, error) {
	var info struct {
		ModelID string `json:"model_id"`
	}
	if e := t.request(ctx, t.EmbedURL+"/info", nil, &info); e != nil {
		return nil, e
	}
	if info.ModelID != t.Model {
		return nil, fmt.Errorf("TEI model %q does not match EMBEDDING_MODEL %q", info.ModelID, t.Model)
	}
	var result [][]float32
	if e := t.request(ctx, t.EmbedURL+"/embed", map[string]any{"inputs": []string{text}, "truncate": false}, &result); e != nil {
		return nil, e
	}
	if len(result) != 1 || len(result[0]) != t.Dimension {
		return nil, fmt.Errorf("embedding dimension mismatch: expected %d", t.Dimension)
	}
	for _, n := range result[0] {
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return nil, fmt.Errorf("invalid embedding")
		}
	}
	return result[0], nil
}
func (t *TEI) Rerank(ctx context.Context, query string, texts []string) (map[int]float64, error) {
	var result []struct {
		Index int     `json:"index"`
		Score float64 `json:"score"`
	}
	if e := t.request(ctx, t.RerankURL+"/rerank", map[string]any{"query": query, "texts": texts, "truncate": false, "return_text": false}, &result); e != nil {
		return nil, e
	}
	out := map[int]float64{}
	for _, r := range result {
		if r.Index < 0 || r.Index >= len(texts) || math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
			return nil, fmt.Errorf("invalid rerank response")
		}
		out[r.Index] = r.Score
	}
	if len(out) != len(texts) {
		return nil, fmt.Errorf("incomplete rerank response")
	}
	return out, nil
}
func (t *TEI) Health(ctx context.Context, url string) bool {
	return t.request(ctx, strings.TrimRight(url, "/")+"/health", nil, nil) == nil
}
