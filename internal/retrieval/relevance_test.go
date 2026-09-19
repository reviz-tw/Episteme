package retrieval

import (
	"context"
	"math"
	"testing"
)

func score(value float64) *float64 { return &value }

func TestKeywordSearchRejectsWeakAndDuplicateJSONResults(t *testing.T) {
	jsonResult := func(id, path, body string, value float64) Result {
		return Result{ChunkID: id, DocumentID: "article", Filename: "article.json", Content: "[" + path + "]\n# " + path + "\n\n" + body,
			Breadcrumbs: []string{path}, RerankScore: score(value)}
	}
	ranked := []Result{
		jsonResult("body", "JSON/background/0", `"候選人私下對話是虛構示範。"`, .033),
		jsonResult("summary", "JSON/summary/0", `"候選人私下對話是虛構示範。"`, .032),
		jsonResult("slug", "JSON/relatedSlugs/0", `"election-message"`, .01),
		jsonResult("finding", "JSON", `"無法作為候選人發言的證據。"`, .005),
		{ChunkID: "guide", DocumentID: "guide", Filename: "guide.md", Content: "BM25 適合精確詞彙；人工確認後才能入庫。", RerankScore: score(.003)},
		jsonResult("metadata", "JSON/seo", `"noindex": false`, .0001),
	}
	for i := range ranked {
		ranked[i].InitialRank = i + 1
	}
	in := Input{Query: "候選人", Alpha: .5, Rerank: true, Limit: 20}
	got := relevantResults(ranked, in)
	if len(got) != 2 || got[0].ChunkID != "body" || got[1].ChunkID != "finding" {
		t.Fatalf("expected complete keyword matches with one repeated paragraph removed: %+v", got)
	}
	if got[1].Rank != 2 || got[1].InitialRank != 4 || got[0].MatchType != "keyword" {
		t.Fatalf("ranks or match explanation incorrect: %+v", got)
	}
	in.IncludeLowRelevance = true
	if all := relevantResults(ranked, in); len(all) != len(ranked) || all[2].MatchType != "candidate" {
		t.Fatalf("debug candidates missing: %+v", all)
	}
}

func TestSemanticThresholdAndSourceAttribution(t *testing.T) {
	ranked := []Result{
		{ChunkID: "a", DocumentID: "a", Content: "在文件審核頁按下載原檔。", RerankScore: score(.9), DenseScore: .7},
		{ChunkID: "b", DocumentID: "b", Content: "在文件審核頁按下載原檔。", RerankScore: score(.8), DenseScore: .6},
		{ChunkID: "c", DocumentID: "c", Content: "今天天氣晴朗。", RerankScore: score(.01), DenseScore: .3},
	}
	in := Input{Query: "如何取回上傳的檔案？", Alpha: .5, Rerank: true, Limit: 20}
	got := relevantResults(ranked, in)
	if len(got) != 2 || got[0].MatchType != "semantic" || got[1].DocumentID != "b" {
		t.Fatalf("semantic matches or separate sources lost: %+v", got)
	}
	in.MinScore = score(.85)
	if got := relevantResults(ranked, in); len(got) != 1 {
		t.Fatalf("custom threshold ignored: %+v", got)
	}
	in.MinScore = score(0)
	if got := relevantResults(ranked, in); len(got) != 3 {
		t.Fatalf("explicit zero threshold ignored: %+v", got)
	}
	in.MinScore = nil
	in.Rerank = false
	if got := relevantResults(ranked, in); len(got) != 2 {
		t.Fatalf("dense fallback: %+v", got)
	}
	in.Alpha = 0
	if got := relevantResults(ranked, in); len(got) != 0 {
		t.Fatalf("pure lexical mode accepted incomplete terms: %+v", got)
	}
	in.Rerank = true
	in.MinScore = score(.95)
	if got := relevantResults(ranked, in); len(got) != 0 {
		t.Fatalf("expected a genuine empty result: %+v", got)
	}
}

func TestCompleteQueryTerms(t *testing.T) {
	for _, test := range []struct {
		query, text string
		match       bool
	}{
		{"候選人", "人工確認內容", false},
		{"候選人", "某候選人的說明", true},
		{"候選人 賄選", "賄選查核：某候選人的說明", true},
		{"候選人 賄選", "某候選人的說明", false},
		{"AI", "He said hello.", false},
		{"AI", "said AI生成音訊", true},
		{"JSON", "json 格式文件", true},
	} {
		if got := containsQuery(test.text, test.query); got != test.match {
			t.Errorf("%q in %q: %v", test.query, test.text, got)
		}
	}
	result := Result{Filename: "data.json", Breadcrumbs: []string{"JSON/候選人"}, Content: "[JSON/候選人]\n# JSON/候選人\n\n1200"}
	if containsQuery(resultBody(result), "候選人") {
		t.Fatal("generated path alone should not count as a content match")
	}
}

func TestInvalidRelevanceThreshold(t *testing.T) {
	s := &Service{}
	for _, minimum := range []float64{-0.1, 1.01, math.NaN(), math.Inf(1)} {
		if _, err := s.Search(context.Background(), Input{Query: "query", Alpha: .5, MinScore: score(minimum)}); err == nil {
			t.Fatalf("accepted invalid threshold %v", minimum)
		}
	}
}
