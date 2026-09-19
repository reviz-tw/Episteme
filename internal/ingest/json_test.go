package ingest

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/reviz-tw/Episteme/internal/chunker"
	"github.com/yuin/goldmark"
)

func TestJSONRecordsAndPrecision(t *testing.T) {
	source, err := JSONMarkdown([]byte(`{"records":[{"z":"第一筆","id":9007199254740993,"a":1.2300},{"z":"第二筆","active":false,"empty":null}],"tail":"結尾"}`))
	if err != nil {
		t.Fatal(err)
	}
	parts := chunker.Split(source, 512)
	if len(parts) != 3 {
		t.Fatalf("expected two records and root tail, got %d: %s", len(parts), source)
	}
	for i, path := range []string{"JSON/records/0", "JSON/records/1", "JSON"} {
		if len(parts[i].Breadcrumbs) != 1 || parts[i].Breadcrumbs[0] != path {
			t.Fatalf("record path: %+v", parts[i])
		}
		if strings.TrimSpace(source[parts[i].Start:parts[i].End]) != parts[i].Raw {
			t.Fatal("source highlight span no longer matches the normalized document")
		}
	}
	if !strings.Contains(parts[0].Raw, "9007199254740993") || !strings.Contains(parts[0].Raw, "1.2300") {
		t.Fatal("numeric precision was lost")
	}
	if strings.Index(source, `"z"`) > strings.Index(source, `"id"`) || strings.Index(source, `"id"`) > strings.Index(source, `"a"`) {
		t.Fatal("field order changed")
	}
	if !strings.Contains(parts[1].Raw, "false") || !strings.Contains(parts[1].Raw, "null") {
		t.Fatal("scalar values were lost")
	}
}

func TestJSONScalarsAndEmptyContainers(t *testing.T) {
	for _, input := range []string{`{}`, `[]`, `null`, `true`, `123`, `"文字"`, `["甲", "乙", {}, []]`, "\ufeff{\"ok\":true}"} {
		t.Run(input, func(t *testing.T) {
			source, err := JSONMarkdown([]byte(input))
			if err != nil || len(chunker.Split(source, 512)) == 0 {
				t.Fatalf("source=%q error=%v", source, err)
			}
		})
	}
}

func TestJSONRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"", `{"a":`, `{"a":1,}`, `[1}`, `{} {}`, "{}\n{}", `{"a":1,"a":2}`, "{\"bad\":\"\xff\"}", strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)} {
		t.Run(input[:min(30, len(input))], func(t *testing.T) {
			if _, err := JSONMarkdown([]byte(input)); err == nil {
				t.Fatal("accepted invalid/unsupported JSON")
			}
		})
	}
	if _, err := JSONMarkdown([]byte("[" + strings.Repeat("0,", 100000) + "0]")); err == nil {
		t.Fatal("value count limit was not enforced")
	}
}

func TestJSONTextCannotInjectMarkdown(t *testing.T) {
	source, err := JSONMarkdown([]byte(`{"line\n# fake\u0000":{"text":"<script>alert(1)</script>\n\n# fake\n` + "```" + `\n![image](https://example.com/x) &amp;"},"a/b~c":{"ok":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	var html bytes.Buffer
	if err = goldmark.Convert([]byte(source), &html); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(source, "\x00") || strings.Contains(html.String(), "<script>") || strings.Contains(html.String(), "<img") || strings.Count(html.String(), "<h1>") != 2 {
		t.Fatalf("JSON content changed document structure: %s", html.String())
	}
	if !strings.Contains(source, "a~1b\\~0c") && !strings.Contains(source, "a\\~1b\\~0c") {
		t.Fatalf("ambiguous path: %s", source)
	}
}

func TestJSONLongTextIsSplit(t *testing.T) {
	source, err := JSONMarkdown([]byte(`{"body":"` + strings.Repeat("長篇中文內容", 300) + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	parts := chunker.Split(source, 512)
	if len(parts) < 3 {
		t.Fatal("long JSON text remained in an atomic code block")
	}
	for _, part := range parts {
		if !utf8.ValidString(part.Raw) || chunker.Tokens(part.Raw) > 512 || len(part.Breadcrumbs) == 0 {
			t.Fatalf("invalid long-text chunk: %+v", part)
		}
	}
}
