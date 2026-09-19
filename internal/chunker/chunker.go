package chunker

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Part struct {
	Raw         string
	Breadcrumbs []string
	Start, End  int
}

// Tokens is a conservative estimate, not a substitute for the model tokenizer.
func Tokens(s string) int {
	c, latin := 0, 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			c++
		} else {
			latin++
		}
	}
	return c + (latin+3)/4
}
func Content(raw string, crumbs, faq []string) string {
	prefix := ""
	if len(crumbs) > 0 {
		prefix = "[" + strings.Join(crumbs, " > ") + "]\n"
	}
	if len(faq) > 0 {
		raw += "\n\nQuestions:\n- " + strings.Join(faq, "\n- ")
	}
	return prefix + raw
}
func first(n ast.Node) int {
	start := int(^uint(0) >> 1)
	if n.Type() != ast.TypeInline {
		for i := 0; i < n.Lines().Len(); i++ {
			if p := n.Lines().At(i).Start; p < start {
				start = p
			}
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if p := first(c); p < start {
			start = p
		}
	}
	return start
}
func lineStart(src []byte, p int) int {
	if p > len(src) {
		p = len(src)
	}
	for p > 0 && src[p-1] != '\n' {
		p--
	}
	return p
}
func Split(source string, limit int) []Part {
	if limit < 32 {
		limit = 512
	}
	src := []byte(source)
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(src))
	type block struct {
		node  ast.Node
		start int
	}
	blocks := []block{}
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		p := lineStart(src, first(n))
		if _, ok := n.(*ast.FencedCodeBlock); ok {
			if p > 0 {
				p = lineStart(src, p-1)
			}
		}
		if len(blocks) == 0 {
			p = 0
		}
		blocks = append(blocks, block{n, p})
	}
	out := []Part{}
	crumbs := []string{}
	levels := []int{}
	pending := ""
	begin, end := 0, 0
	flush := func() {
		if strings.TrimSpace(pending) != "" {
			out = append(out, Part{strings.TrimSpace(pending), append([]string{}, crumbs...), begin, end})
		}
		pending = ""
	}
	for i, b := range blocks {
		stop := len(src)
		if i+1 < len(blocks) {
			stop = blocks[i+1].start
		}
		if stop < b.start {
			continue
		}
		if h, ok := b.node.(*ast.Heading); ok {
			flush()
			for len(levels) > 0 && levels[len(levels)-1] >= h.Level {
				levels = levels[:len(levels)-1]
				crumbs = crumbs[:len(crumbs)-1]
			}
			levels = append(levels, h.Level)
			crumbs = append(crumbs, string(h.Text(src)))
		}
		raw := string(src[b.start:stop])
		if Tokens(pending+raw) > limit {
			flush()
		}
		// Tables, lists and code remain atomic, even if they exceed the configured size.
		if _, ok := b.node.(*ast.Paragraph); ok && Tokens(raw) > limit {
			offset := b.start
			var piece strings.Builder
			cjk, latin := 0, 0
			for _, r := range raw {
				piece.WriteRune(r)
				if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
					cjk++
				} else {
					latin++
				}
				if cjk+(latin+3)/4 >= limit {
					out = append(out, Part{strings.TrimSpace(piece.String()), append([]string{}, crumbs...), offset, offset + piece.Len()})
					offset += piece.Len()
					piece.Reset()
					cjk, latin = 0, 0
				}
			}
			if strings.TrimSpace(piece.String()) != "" {
				out = append(out, Part{strings.TrimSpace(piece.String()), append([]string{}, crumbs...), offset, stop})
			}
			continue
		}
		if pending == "" {
			begin = b.start
		}
		pending += raw
		end = stop
	}
	flush()
	return out
}

// SplitAt uses Unicode code-point offsets. The browser converts its UTF-16 cursor.
func SplitAt(s string, pos int) (string, string, bool) {
	if pos <= 0 || pos >= utf8.RuneCountInString(s) {
		return "", "", false
	}
	r := []rune(s)
	a, b := strings.TrimSpace(string(r[:pos])), strings.TrimSpace(string(r[pos:]))
	return a, b, a != "" && b != ""
}
