package retrieval

import (
	"hash/fnv"
	"sort"
	"strings"
	"unicode"
)

// Stable multilingual lexical features: Latin words and Han unigrams/bigrams.
// BM25 uses k1=1.2, b=.75 and a fixed reference length (256 lexical terms),
// so an edit does not require re-embedding every other document. Qdrant supplies IDF.
func Sparse(s string, query bool) ([]uint32, []float32) {
	terms := []string{}
	word := ""
	previous := rune(0)
	flush := func() {
		if word != "" {
			terms = append(terms, word)
			word = ""
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.Is(unicode.Han, r) {
			flush()
			terms = append(terms, string(r))
			if previous != 0 {
				terms = append(terms, string([]rune{previous, r}))
			}
			previous = r
		} else {
			previous = 0
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				word += string(r)
			} else {
				flush()
			}
		}
	}
	flush()
	counts := map[uint32]float32{}
	for _, term := range terms {
		h := fnv.New32a()
		_, _ = h.Write([]byte(term))
		counts[h.Sum32()]++
	}
	ids := []uint32{}
	for k := range counts {
		ids = append(ids, k)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	values := []float32{}
	for _, id := range ids {
		tf := counts[id]
		v := float32(1)
		if !query {
			v = tf * 2.2 / (tf + 1.2*(.25+.75*float32(len(terms))/256))
		}
		values = append(values, v)
	}
	return ids, values
}
