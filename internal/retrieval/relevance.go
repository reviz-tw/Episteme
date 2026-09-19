package retrieval

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Scores are model-specific screening defaults, not calibrated probabilities.
// Complete lexical matches remain eligible: short keyword queries can receive
// very low cross-encoder scores even when the passage explicitly mentions them.
func relevantResults(ranked []Result, in Input) []Result {
	threshold := 0.5
	if in.Rerank {
		threshold = 0.1
	}
	if in.MinScore != nil {
		threshold = *in.MinScore
	}
	seen := map[string]bool{}
	out := []Result{}
	for _, result := range ranked {
		body := resultBody(result)
		score := result.DenseScore
		semantic := in.Alpha > 0
		if in.Rerank {
			semantic = result.RerankScore != nil
			if semantic {
				score = *result.RerankScore
			}
		}
		result.MatchType = "candidate"
		if containsQuery(body, in.Query) {
			result.MatchType = "keyword"
		} else if semantic && score >= threshold {
			result.MatchType = "semantic"
		}
		if !in.IncludeLowRelevance {
			if result.MatchType == "candidate" {
				continue
			}
			// Deduplicate within a source document, retaining the highest-ranked
			// occurrence and its source link. Other documents remain attributable.
			key := result.DocumentID + "\x00" + strings.Join(strings.Fields(body), " ")
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		result.Rank = len(out) + 1
		out = append(out, result)
		if len(out) == in.Limit {
			break
		}
	}
	return out
}

func containsQuery(body, query string) bool {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return false
	}
	body = strings.ToLower(body)
	for _, term := range terms {
		if !containsTerm(body, term) {
			return false
		}
	}
	return true
}

func containsTerm(body, term string) bool {
	wordRune := func(r rune) bool { return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r) }
	first, _ := utf8.DecodeRuneInString(term)
	last, _ := utf8.DecodeLastRuneInString(term)
	for offset := 0; offset < len(body); {
		index := strings.Index(body[offset:], term)
		if index < 0 {
			return false
		}
		start, end := offset+index, offset+index+len(term)
		before, _ := utf8.DecodeLastRuneInString(body[:start])
		after, _ := utf8.DecodeRuneInString(body[end:])
		if !(wordRune(first) && wordRune(before)) && !(wordRune(last) && wordRune(after)) {
			return true
		}
		offset = end
	}
	return false
}

func resultBody(result Result) string {
	body := strings.TrimPrefix(result.Content, "["+strings.Join(result.Breadcrumbs, " > ")+"]\n")
	// JSON paths are generated structure, not the record's content. Removing
	// them also exposes identical paragraphs repeated in summary/body fields.
	if strings.HasSuffix(strings.ToLower(result.Filename), ".json") && len(result.Breadcrumbs) == 1 {
		path := result.Breadcrumbs[0]
		if path == "JSON" || strings.HasPrefix(path, "JSON/") {
			body = strings.TrimPrefix(body, "# "+path+"\n")
		}
	}
	return strings.TrimSpace(body)
}
