package storage

import (
	"reflect"
	"strings"
	"testing"
)

func TestDocumentMetadataValidation(t *testing.T) {
	got, err := NormalizeMetadata(DocumentMetadata{Title: " 文件資料 ", SourceURL: "https://example.org/article", PublishedAt: "2026-09-19", Tags: []string{" 查核 ", "查核", "", "中文"}, Custom: map[string]string{" 語言 ": " 繁體中文 "}})
	if err != nil || got.Title != "文件資料" || !reflect.DeepEqual(got.Tags, []string{"查核", "中文"}) || got.Custom["語言"] != "繁體中文" {
		t.Fatalf("normalization: %+v %v", got, err)
	}
	for name, invalid := range map[string]DocumentMetadata{
		"unsafe URL":               {SourceURL: "javascript:alert(1)"},
		"relative URL":             {SourceURL: "/article"},
		"date":                     {PublishedAt: "2026-02-30"},
		"null byte":                {Title: "bad\x00title"},
		"blank key":                {Custom: map[string]string{" ": "value"}},
		"duplicate normalized key": {Custom: map[string]string{"tag": "a", " tag ": "b"}},
		"oversize value":           {Custom: map[string]string{"key": strings.Repeat("文", 2001)}},
		"oversize metadata":        {Custom: map[string]string{"a": strings.Repeat("文", 2000), "b": strings.Repeat("文", 2000), "c": strings.Repeat("文", 2000)}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeMetadata(invalid); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
}
