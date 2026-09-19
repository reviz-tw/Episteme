"use client";
import { useRef, useState } from "react";
import Link from "next/link";
import {
  Search,
  ArrowUpRight,
  ArrowUp,
  ArrowDown,
  SlidersHorizontal,
  Sparkles,
} from "lucide-react";
import { Shell } from "@/components/shell";
import { Button } from "@/components/ui/button";
import { Markdown } from "@/components/markdown";
import { MetadataView } from "@/components/document-metadata";
import { post, SearchResult } from "@/lib/api";
export default function PlaygroundPage() {
  return (
    <Shell>
      <Playground />
    </Shell>
  );
}
function Playground() {
  const [query, setQuery] = useState(""),
    [alpha, setAlpha] = useState(0.5),
    [rerank, setRerank] = useState(true),
    [minRerankScore, setMinRerankScore] = useState(0.1),
    [minDenseScore, setMinDenseScore] = useState(0.5),
    [includeLowRelevance, setIncludeLowRelevance] = useState(false),
    [searchedAll, setSearchedAll] = useState(false),
    [results, setResults] = useState<SearchResult[] | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [elapsed, setElapsed] = useState(0),
    [searchedQuery, setSearchedQuery] = useState("");
  const request = useRef(0);
  const minScore = rerank ? minRerankScore : minDenseScore;
  const search = async (
    a = alpha,
    re = rerank,
    minimum = re ? minRerankScore : minDenseScore,
    all = includeLowRelevance,
  ) => {
    if (!query.trim()) return;
    const sequence = ++request.current;
    setBusy(true);
    setError("");
    const start = performance.now();
    try {
      const data = await post<SearchResult[]>("/retrieval/search", {
        query,
        alpha: a,
        rerank_enabled: re,
        limit: 20,
        min_score: minimum,
        include_low_relevance: all,
      });
      if (sequence !== request.current) return;
      setResults(data);
      setElapsed(performance.now() - start);
      setSearchedQuery(query);
      setSearchedAll(all);
    } catch (e) {
      if (sequence === request.current) {
        setError((e as Error).message);
        setResults(null);
      }
    } finally {
      if (sequence === request.current) setBusy(false);
    }
  };
  return (
    <div className="page">
      <div className="page-heading">
        <div>
          <span className="eyebrow">ASK. COMPARE. UNDERSTAND.</span>
          <h1>
            檢索調試場<span className="heading-dot">.</span>
          </h1>
          <p className="muted">看見每個答案的來源，找到最適合你的檢索方式。</p>
        </div>
        <span className="large-symbol">
          <Sparkles size={30} />
        </span>
      </div>
      <form
        className="query-form"
        onSubmit={(e) => {
          e.preventDefault();
          void search();
        }}
      >
        <Search size={22} />
        <input
          aria-label="搜尋知識庫"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="你想從知識庫中找到什麼？"
          maxLength={8000}
          required
        />
        <Button type="submit" disabled={busy || !query.trim()}>
          {busy ? "搜尋中…" : "搜尋知識庫"}
          <ArrowUpRight size={15} />
        </Button>
      </form>
      <div className="playground-grid">
        <aside className="parameters">
          <h2>
            <SlidersHorizontal size={17} />
            檢索參數
          </h2>
          <label className="alpha-heading">
            混合檢索權重<strong>{alpha.toFixed(2)}</strong>
          </label>
          <input
            aria-label="Alpha 混合權重"
            type="range"
            min="0"
            max="1"
            step="0.05"
            value={alpha}
            onChange={(e) => setAlpha(Number(e.target.value))}
            onPointerUp={() => {
              if (results) void search();
            }}
            onKeyUp={() => {
              if (results) void search();
            }}
          />
          <div className="range-labels">
            <span>BM25 關鍵字</span>
            <span>Dense 語意</span>
          </div>
          <p>向左保留精確詞彙，向右探索語意相近的內容。</p>
          <hr />
          <label className="toggle-label">
            <div>
              <strong>啟用 Reranker</strong>
              <small>讓最相關的切片往前排</small>
            </div>
            <input
              type="checkbox"
              role="switch"
              checked={rerank}
              onChange={(e) => {
                setRerank(e.target.checked);
                if (results) void search(alpha, e.target.checked);
              }}
            />
          </label>
          <div className="relevance-options">
            <label className="alpha-heading" htmlFor="relevance-threshold">
              {rerank ? "最低重排分數" : "最低語意相似度"}
              <strong>{minScore.toFixed(2)}</strong>
            </label>
            <input
              id="relevance-threshold"
              type="range"
              min="0"
              max="1"
              step="0.01"
              value={minScore}
              disabled={includeLowRelevance || (!rerank && alpha === 0)}
              onChange={(e) =>
                (rerank ? setMinRerankScore : setMinDenseScore)(
                  Number(e.target.value),
                )
              }
              onPointerUp={() => {
                if (results) void search();
              }}
              onKeyUp={() => {
                if (results) void search();
              }}
            />
            <p>
              {!rerank && alpha === 0
                ? "純關鍵字模式保留完整命中查詢詞的內容。"
                : "提高門檻可減少弱相關結果；完整命中查詢詞的內容仍會保留。分數不代表正確率。"}
            </p>
            <label className="toggle-label candidate-toggle">
              <div>
                <strong>顯示全部候選</strong>
                <small>包含低相關及重複內容，供調試比較</small>
              </div>
              <input
                type="checkbox"
                role="switch"
                checked={includeLowRelevance}
                onChange={(e) => {
                  setIncludeLowRelevance(e.target.checked);
                  if (results)
                    void search(alpha, rerank, minScore, e.target.checked);
                }}
              />
            </label>
          </div>
          <div className="parameter-note">
            <span className="eyebrow">HOW IT WORKS</span>
            <p>
              兩路各召回候選，再依 Alpha 權重合併排名。最多 20
              個候選交由重排模型比較，再篩選相關結果並合併同一文件的重複段落。
            </p>
            <small>只搜尋已入庫且未排除的內容。</small>
          </div>
        </aside>
        <section className="results">
          {error && (
            <div className="notice error" role="alert">
              {error}
            </div>
          )}
          {results && (
            <div className="section-heading">
              <h2>
                {searchedAll ? "候選結果" : "檢索結果"}{" "}
                <span className="muted">{results.length}</span>
              </h2>
              <small>
                {Math.round(elapsed)} ms · {searchedQuery}
              </small>
            </div>
          )}
          {results && (
            <p className="search-filter-note">
              {searchedAll
                ? "正在顯示全部候選，包含低相關與重複內容。"
                : "已篩選相關結果；同一文件的相同段落只顯示一次。"}
            </p>
          )}
          {results?.map((r) => (
            <article className="result-card" key={r.chunk_id}>
              <header>
                <div>
                  <span className="result-rank">
                    {String(r.rank).padStart(2, "0")}
                  </span>
                  <div>
                    <strong>{r.filename}</strong>
                    <small>{r.breadcrumbs.join(" / ")}</small>
                  </div>
                </div>
                <span
                  className={`rank-change ${r.initial_rank > r.rank ? "up" : ""}`}
                >
                  {r.initial_rank > r.rank ? (
                    <ArrowUp size={13} />
                  ) : r.initial_rank < r.rank ? (
                    <ArrowDown size={13} />
                  ) : null}
                  {r.initial_rank === r.rank
                    ? "名次不變"
                    : `#${r.initial_rank} → #${r.rank}`}
                </span>
              </header>
              <Markdown>{r.content}</Markdown>
              <MetadataView metadata={r.document_metadata || {}} />
              <footer>
                <span>
                  <strong className={`match-label ${r.match_type}`}>
                    {r.match_type === "keyword"
                      ? "關鍵字符合"
                      : r.match_type === "semantic"
                        ? "語意相關"
                        : "低相關候選"}
                  </strong>
                  Dense {r.dense_score.toFixed(3)}
                  <i />
                  BM25 {r.sparse_score.toFixed(3)}
                  <i />
                  融合 {r.score.toFixed(4)}
                  {r.rerank_score !== null && (
                    <>
                      {" "}
                      <i />
                      重排 {r.rerank_score.toFixed(3)}
                    </>
                  )}
                </span>
                <Link
                  href={`/documents/${r.document_id}/review?chunk=${r.chunk_id}`}
                >
                  查看來源
                  <ArrowUpRight size={14} />
                </Link>
              </footer>
            </article>
          ))}
          {(!results || results.length === 0) && (
            <div className="empty-state playground-empty">
              <Search size={38} />
              <h3>{results ? "沒有符合的已入庫內容" : "從一個好問題開始"}</h3>
              <p>
                {results
                  ? "試試其他關鍵字、降低相關性門檻，或開啟「顯示全部候選」比較。"
                  : "輸入問題，比較關鍵字、語意檢索與重排之間的差異。"}
              </p>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
