export const API = "/api/v1";
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(API + path, {
    ...init,
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401 && !path.startsWith("/auth/"))
      window.location.assign("/login");
    throw new Error(body.error || `HTTP ${response.status}`);
  }
  return body as T;
}
export const post = <T>(path: string, body: unknown = {}) =>
  api<T>(path, { method: "POST", body: JSON.stringify(body) });
export type DocumentMetadata = {
  title?: string;
  source?: string;
  source_url?: string;
  author?: string;
  published_at?: string;
  tags?: string[];
  custom?: Record<string, string>;
};
export type Doc = {
  id: string;
  filename: string;
  mime_type: string;
  source_markdown: string;
  status: string;
  chunk_size: number;
  revision: number;
  total_tokens: number;
  chunk_count: number;
  reviewed_count: number;
  created_at: string;
  updated_at: string;
  metadata: DocumentMetadata;
  metadata_generation: MetadataGeneration | null;
};
export type MetadataGeneration = {
  status: "queued" | "running" | "completed" | "failed" | "skipped";
  model: string;
  attempts: number;
  error: string;
  sampled: boolean;
  updated_at: string;
};
export type Chunk = {
  id: string;
  document_id: string;
  chunk_index: number;
  raw_markdown: string;
  content: string;
  breadcrumbs: string[];
  token_count: number;
  is_reviewed: boolean;
  is_excluded: boolean;
  is_dirty: boolean;
  metadata: { faq?: string[] };
  source_start: number;
  source_end: number;
  revision: number;
  indexed_revision: number;
  indexed_content: string;
  indexed_model: string;
};
export function estimateTokens(c: Chunk) {
  const questions = c.metadata.faq?.filter(Boolean) || [];
  const content =
    (c.breadcrumbs.length ? `[${c.breadcrumbs.join(" > ")}]\n` : "") +
    c.raw_markdown +
    (questions.length ? "\n\nQuestions:\n- " + questions.join("\n- ") : "");
  const characters = Array.from(content);
  const cjk = characters.filter((r) =>
    /\p{Script=Han}|\p{Script=Hiragana}|\p{Script=Katakana}/u.test(r),
  ).length;
  return cjk + Math.ceil((characters.length - cjk) / 4);
}
export type Job = {
  id: string;
  kind: string;
  status: string;
  total: number;
  completed: number;
  error: string;
  created_at: string;
};
export type Dashboard = {
  metadata_enabled: boolean;
  metadata_model: string;
  documents: number;
  indexed_chunks: number;
  draft_chunks: number;
  total_tokens: number;
  vector_bytes_estimate: number;
  health: Record<string, boolean>;
  model: string;
  dimension: number;
  collection: string;
};
export type SearchResult = {
  chunk_id: string;
  document_id: string;
  filename: string;
  content: string;
  breadcrumbs: string[];
  score: number;
  dense_score: number;
  sparse_score: number;
  rerank_score: number | null;
  initial_rank: number;
  rank: number;
  match_type: "keyword" | "semantic" | "candidate";
  document_metadata: DocumentMetadata;
};
export const statusLabel: Record<string, string> = {
  draft: "待審核",
  reviewed: "已審核",
  stale: "待更新",
  indexing: "索引中",
  indexed: "已索引",
  failed: "失敗",
  queued: "排程中",
  running: "處理中",
  paused: "已暫停",
  completed: "已完成",
  cancelled: "已取消",
};
