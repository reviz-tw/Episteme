package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestASTAndUnicode(t *testing.T) {
	source := "# 使用手冊\n\n前言。\n\n## 設定\n\n| 欄位 | 值 |\n| --- | --- |\n| 地區 | 台灣 |\n\n```go\n" + strings.Repeat("fmt.Println(\"中文\")\n", 12) + "```\n\n## 問答\n\n" + strings.Repeat("知識需要查核。", 60)
	parts := Split(source, 32)
	if len(parts) < 4 {
		t.Fatalf("expected several parts: %v", parts)
	}
	for _, p := range parts {
		if !utf8.ValidString(p.Raw) {
			t.Fatal("broken Unicode")
		}
		if p.Start < 0 || p.End > len(source) || p.End < p.Start {
			t.Fatalf("invalid original range %+v", p)
		}
		if strings.Contains(p.Raw, "fmt.Println") && !strings.Contains(p.Raw, "```go") {
			t.Fatal("split code fence")
		}
		if strings.Contains(p.Raw, "| 地區") && !strings.Contains(p.Raw, "| 欄位") {
			t.Fatal("split table")
		}
		if strings.Contains(p.Raw, "| 欄位") && (len(p.Breadcrumbs) != 2 || p.Breadcrumbs[1] != "設定") {
			t.Fatalf("breadcrumbs: %+v", p)
		}
	}
}
func TestSplitAtRuneBoundary(t *testing.T) {
	a, b, ok := SplitAt("你好🌱世界", 3)
	if !ok || a != "你好🌱" || b != "世界" {
		t.Fatal(a, b, ok)
	}
	if _, _, ok = SplitAt("hello", 0); ok {
		t.Fatal("empty split allowed")
	}
}
func TestOriginalNoLostText(t *testing.T) {
	for _, src := range []string{"hello world\n", "# One\n\nA paragraph.\n\n## Two\n\nAnother.\n", "```\n\n```\n", "---\n\nhello\n"} {
		parts := Split(src, 512)
		joined := ""
		for _, p := range parts {
			joined += p.Raw
		}
		normalize := func(s string) string { return strings.Join(strings.Fields(s), "") }
		if normalize(joined) != normalize(src) {
			t.Fatalf("text lost: source=%q result=%q", src, joined)
		}
	}
}
