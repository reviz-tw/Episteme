// Package ingest converts source formats into the Markdown used by review and indexing.
package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxJSONText = 20 << 20

type field struct {
	name  string
	value node
}

type node struct {
	kind   json.Delim
	fields []field
	items  []node
	value  any
}

// JSONMarkdown preserves field order and numeric precision. Container paths become
// section boundaries, so separate records cannot accidentally share a chunk.
// Scalars remain paragraphs (not code fences), allowing long text to be split.
func JSONMarkdown(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", errors.New("JSON 必須使用 UTF-8 編碼")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	count := 0
	root, err := readNode(dec, 0, &count)
	if err != nil {
		return "", fmt.Errorf("JSON 格式不正確：%w", err)
	}
	if _, err = dec.Token(); err != io.EOF {
		return "", errors.New("JSON 檔案只能包含一個完整值；多筆資料請放在陣列中")
	}
	var out strings.Builder
	if err = renderNode(&out, root, ""); err != nil {
		return "", err
	}
	return out.String(), nil
}

func readNode(dec *json.Decoder, depth int, count *int) (node, error) {
	*count = *count + 1
	if depth > 64 || *count > 100000 {
		return node{}, errors.New("JSON 結構過大，請拆成較小的檔案（最多 64 層、100,000 個值）")
	}
	token, err := dec.Token()
	if err != nil {
		return node{}, err
	}
	kind, container := token.(json.Delim)
	if !container {
		return node{value: token}, nil
	}
	n := node{kind: kind}
	seen := map[string]bool{}
	for dec.More() {
		name := ""
		if kind == '{' {
			key, err := dec.Token()
			if err != nil {
				return node{}, err
			}
			name = key.(string)
			if seen[name] {
				return node{}, errors.New("同一物件含有重複欄位名稱，請先修正")
			}
			seen[name] = true
		}
		child, err := readNode(dec, depth+1, count)
		if err != nil {
			return node{}, err
		}
		if kind == '{' {
			n.fields = append(n.fields, field{name: name, value: child})
		} else {
			n.items = append(n.items, child)
		}
	}
	_, err = dec.Token() // Decoder validates the matching closing delimiter.
	return n, err
}

func renderNode(out *strings.Builder, n node, path string) error {
	section := false
	write := func(label, value string) error {
		if !section {
			out.WriteString("# JSON" + escapeMarkdown(path) + "\n\n")
			section = true
		}
		if label != "" {
			out.WriteString("**" + escapeMarkdown(label) + "**: ")
		}
		out.WriteString(escapeMarkdown(value) + "\n\n")
		if out.Len() > maxJSONText {
			return errors.New("JSON 轉換後的文字超過 20 MB，請拆成較小的檔案")
		}
		return nil
	}
	child := func(label, segment string, value node) error {
		if len(value.fields)+len(value.items) > 0 {
			section = false
			return renderNode(out, value, path+"/"+segment)
		}
		return write(label, scalar(value))
	}
	for _, f := range n.fields {
		segment := strings.ReplaceAll(strings.ReplaceAll(f.name, "~", "~0"), "/", "~1")
		if err := child(jsonText(f.name), segment, f.value); err != nil {
			return err
		}
	}
	for i, item := range n.items {
		index := strconv.Itoa(i)
		// Each array entry is a separate record, including scalar entries.
		if err := renderNode(out, item, path+"/"+index); err != nil {
			return err
		}
	}
	if len(n.fields)+len(n.items) == 0 {
		return write("", scalar(n))
	}
	return nil
}

func scalar(n node) string {
	switch n.kind {
	case '{':
		return "{}"
	case '[':
		return "[]"
	default:
		return jsonText(n.value)
	}
}

func jsonText(value any) string {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
	return strings.TrimSuffix(out.String(), "\n")
}

func escapeMarkdown(s string) string {
	var out strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&out, `\u%04x`, r)
				continue
			}
			if strings.ContainsRune("\\`*_{}[]<>#+!|&~", r) {
				out.WriteByte('\\')
			}
			out.WriteRune(r)
		}
	}
	return out.String()
}
