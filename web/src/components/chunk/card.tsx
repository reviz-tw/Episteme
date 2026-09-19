"use client";
import { useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { markdown } from "@codemirror/lang-markdown";
import {
  Check,
  Scissors,
  Combine,
  Ban,
  Pencil,
  RotateCw,
  MessageCirclePlus,
  GitCompareArrows,
} from "lucide-react";
import { Chunk } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Markdown } from "@/components/markdown";
type Props = {
  chunk: Chunk;
  dirty: boolean;
  limit: number;
  selected: boolean;
  busy: boolean;
  onSelect: () => void;
  onChange: (c: Chunk) => void;
  onSplit: (position: number) => void;
  onMerge: () => void;
  onIndex: () => void;
  hasNext: boolean;
};
export function ChunkCard({
  chunk: c,
  dirty,
  limit,
  selected,
  busy,
  onSelect,
  onChange,
  onSplit,
  onMerge,
  onIndex,
  hasNext,
}: Props) {
  const [editing, setEditing] = useState(false),
    [cursor, setCursor] = useState(0),
    [faq, setFAQ] = useState(false),
    [diff, setDiff] = useState(false),
    [crumbs, setCrumbs] = useState(false),
    [crumbText, setCrumbText] = useState(c.breadcrumbs.join(" / "));
  const change = (patch: Partial<Chunk>, content = false) =>
    onChange({ ...c, ...patch, ...(content ? { is_reviewed: false } : {}) });
  return (
    <article
      data-chunk-id={c.id}
      className={`chunk-card ${selected ? "selected" : ""} ${c.is_excluded ? "excluded" : ""}`}
      onClick={onSelect}
    >
      <header>
        <div>
          <span className="chunk-number">
            #{String(c.chunk_index + 1).padStart(2, "0")}
          </span>
          <span className={c.token_count > limit ? "error-text" : "muted"}>
            {c.token_count} tokens
            {c.token_count > limit ? " · 超過設定長度" : ""}
          </span>
        </div>
        <span
          className={`badge ${c.is_excluded ? "excluded" : c.is_reviewed ? "indexed" : "draft"}`}
        >
          {c.is_excluded ? "已排除" : c.is_reviewed ? "已確認" : "待確認"}
          {dirty ? " · 未儲存" : ""}
        </span>
      </header>
      <button
        className="breadcrumbs-edit"
        disabled={busy}
        onClick={() => {
          if (!crumbs) setCrumbText(c.breadcrumbs.join(" / "));
          setCrumbs(!crumbs);
        }}
      >
        {c.breadcrumbs.join(" / ") || "新增章節路徑"}
        <Pencil size={11} />
      </button>
      {crumbs && (
        <label className="compact-label">
          章節層級（以 / 分隔）
          <input
            value={crumbText}
            disabled={busy}
            onChange={(e) => {
              setCrumbText(e.target.value);
              change(
                {
                  breadcrumbs: e.target.value
                    .split("/")
                    .map((s) => s.trim())
                    .filter(Boolean),
                },
                true,
              );
            }}
          />
        </label>
      )}
      {editing ? (
        <div className="editor">
          <CodeMirror
            aria-label={`編輯切片 ${c.chunk_index + 1}`}
            value={c.raw_markdown}
            extensions={[markdown()]}
            editable={!busy}
            basicSetup={{ lineNumbers: true, foldGutter: false }}
            onChange={(value) => change({ raw_markdown: value }, true)}
            onUpdate={(view) =>
              setCursor(
                Array.from(
                  view.state.doc
                    .toString()
                    .slice(0, view.state.selection.main.head),
                ).length,
              )
            }
          />
          <Button variant="ghost" size="sm" onClick={() => setEditing(false)}>
            完成編輯
          </Button>
        </div>
      ) : (
        <button
          className="chunk-preview"
          disabled={busy}
          aria-label={`編輯切片 ${c.chunk_index + 1}`}
          onClick={() => setEditing(true)}
        >
          <Markdown>{c.raw_markdown}</Markdown>
          <span className="edit-hint">
            <Pencil size={12} />
            點擊編輯內容
          </span>
        </button>
      )}
      {faq && (
        <label className="compact-label">
          可回答的問題 · 每行一題
          <textarea
            value={(c.metadata.faq || []).join("\n")}
            disabled={busy}
            placeholder="這段內容可以回答哪些問題？"
            onChange={(e) =>
              change(
                {
                  metadata: { ...c.metadata, faq: e.target.value.split("\n") },
                },
                true,
              )
            }
          />
        </label>
      )}
      {diff && (
        <div className="diff-panel">
          <div>
            <small>上次入庫內容</small>
            <pre>{c.indexed_content || "尚未入庫"}</pre>
          </div>
          <div>
            <small>目前草稿</small>
            <pre>{c.raw_markdown}</pre>
          </div>
        </div>
      )}
      <footer>
        <div className="chunk-tools">
          <Button
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => change({ is_excluded: !c.is_excluded })}
          >
            <Ban size={13} />
            {c.is_excluded ? "取消排除" : "排除"}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={busy || !hasNext}
            onClick={onMerge}
          >
            <Combine size={13} />
            向下合併
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={busy || !editing || cursor === 0}
            onClick={() => onSplit(cursor)}
          >
            <Scissors size={13} />
            游標切分
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => setFAQ(!faq)}
          >
            <MessageCirclePlus size={13} />
            FAQ
            {c.metadata.faq?.filter(Boolean).length
              ? ` (${c.metadata.faq.filter(Boolean).length})`
              : ""}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setDiff(!diff)}>
            <GitCompareArrows size={13} />
            差異
          </Button>
          {c.indexed_revision > 0 && (
            <Button
              variant="ghost"
              size="sm"
              disabled={busy || !c.is_reviewed || c.is_excluded}
              onClick={onIndex}
            >
              <RotateCw size={13} />
              局部更新
            </Button>
          )}
        </div>
        <Button
          size="sm"
          variant={c.is_reviewed ? "outline" : "default"}
          disabled={busy || c.is_excluded}
          onClick={() => change({ is_reviewed: !c.is_reviewed })}
        >
          <Check size={14} />
          {c.is_reviewed ? "取消確認" : "確認"}
        </Button>
      </footer>
    </article>
  );
}
