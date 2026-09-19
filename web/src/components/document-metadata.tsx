"use client";

import { Plus, Tag, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DocumentMetadata, MetadataGeneration } from "@/lib/api";

const fields = [
  ["title", "文件標題", "text", 300],
  ["source", "來源／機構", "text", 200],
  ["source_url", "來源網址", "url", 2048],
  ["author", "作者", "text", 200],
  ["published_at", "發布日期", "text", 10],
] as const;

export type MetadataForm = {
  title: string;
  source: string;
  source_url: string;
  author: string;
  published_at: string;
  tagsText: string;
  customRows: { id: string; key: string; value: string }[];
};

function toForm(metadata: DocumentMetadata): MetadataForm {
  return {
    title: metadata.title || "",
    source: metadata.source || "",
    source_url: metadata.source_url || "",
    author: metadata.author || "",
    published_at: metadata.published_at || "",
    tagsText: metadata.tags?.join(", ") || "",
    customRows: Object.entries(metadata.custom || {}).map(
      ([key, value], i) => ({ id: String(i), key, value }),
    ),
  };
}

export function metadataFromForm(form: MetadataForm): DocumentMetadata {
  const names = new Set<string>();
  const custom: [string, string][] = [];
  for (const row of form.customRows) {
    const key = row.key.trim(),
      value = row.value.trim();
    if (!key && !value) continue;
    if (!key) throw new Error("請填寫自訂欄位名稱，或移除空白欄位。");
    if (names.has(key))
      throw new Error(`自訂欄位「${key}」重複，請使用不同名稱。`);
    names.add(key);
    custom.push([key, value]);
  }
  return {
    title: form.title.trim(),
    source: form.source.trim(),
    source_url: form.source_url.trim(),
    author: form.author.trim(),
    published_at: form.published_at,
    tags: [
      ...new Set(
        form.tagsText
          .split(/[,，\n]/)
          .map((s) => s.trim())
          .filter(Boolean),
      ),
    ],
    custom: Object.fromEntries(custom),
  };
}

export function MetadataEditor({
  metadata,
  draft,
  busy,
  onChange,
  onReset,
  generation,
  onGenerate,
  generateDisabled,
}: {
  metadata: DocumentMetadata;
  draft?: MetadataForm;
  busy: boolean;
  onChange: (form: MetadataForm) => void;
  onReset: () => void;
  generation?: MetadataGeneration | null;
  onGenerate: () => void;
  generateDisabled: boolean;
}) {
  const form = draft || toForm(metadata);
  const pending =
    generation?.status === "queued" || generation?.status === "running";
  const generationText = !generation
    ? "可用 AI 自動補齊空白欄位，再由你確認。"
    : {
        queued: generation.attempts
          ? "模型暫時無法完成，稍後會自動重試。"
          : "已排程，自動擷取即將開始。",
        running: "正在擷取文件資料，完成後會自動填入草稿。",
        completed:
          "AI 已完成預填，請檢查資料是否正確；未找到依據的欄位會留空。",
        failed: "自動擷取失敗；文件已保留，可手動填寫或重試。",
        skipped: "擷取期間文件資料已被修改，已保留你的修改。",
      }[generation.status];
  return (
    <details className="document-metadata-editor">
      <summary>
        <span>
          <Tag size={16} />
          文件資料（Metadata）
        </span>
        <small>
          {draft
            ? "未儲存"
            : pending
              ? "AI 擷取中…"
              : metadata.title ||
                metadata.source ||
                "標題、來源、作者、日期與標籤"}
        </small>
      </summary>
      <div className="metadata-editor-body">
        <div className="metadata-generation" role="status">
          <div>
            <p>{generationText}</p>
            {generation && (
              <small>
                模型：{generation.model}
                {generation.sampled
                  ? " · 文件較長，本次使用開頭與結尾節錄。"
                  : ""}
              </small>
            )}
            {generation?.error && (
              <p className="metadata-generation-error">{generation.error}</p>
            )}
          </div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={busy || pending || generateDisabled}
            onClick={onGenerate}
          >
            {pending
              ? "擷取中…"
              : generation?.status === "failed"
                ? "重試自動擷取"
                : "自動補齊空白欄位"}
          </Button>
        </div>
        <p>
          套用到這份文件的所有切片。儲存草稿後，按「核可並入庫」更新搜尋結果中的文件資料。
        </p>
        <fieldset disabled={busy || pending}>
          <div className="metadata-fields">
            {fields.map(([key, label, type, maxLength]) => (
              <label key={key}>
                {label}
                <input
                  type={type}
                  value={form[key]}
                  maxLength={maxLength}
                  placeholder={
                    key === "published_at" ? "YYYY-MM-DD" : undefined
                  }
                  onChange={(e) => onChange({ ...form, [key]: e.target.value })}
                />
              </label>
            ))}
            <label>
              標籤（以逗號分隔）
              <input
                value={form.tagsText}
                placeholder="例如：選舉, 查核, 示範資料"
                onChange={(e) =>
                  onChange({ ...form, tagsText: e.target.value })
                }
              />
            </label>
          </div>
          <div className="metadata-custom-heading">
            <strong>自訂欄位</strong>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy || pending || form.customRows.length >= 30}
              onClick={() =>
                onChange({
                  ...form,
                  customRows: [
                    ...form.customRows,
                    { id: crypto.randomUUID(), key: "", value: "" },
                  ],
                })
              }
            >
              <Plus size={13} />
              新增欄位
            </Button>
          </div>
          {form.customRows.map((row, index) => (
            <div className="metadata-custom-row" key={row.id}>
              <input
                aria-label={`自訂欄位 ${index + 1} 名稱`}
                placeholder="欄位名稱，例如：語言"
                maxLength={80}
                value={row.key}
                onChange={(e) =>
                  onChange({
                    ...form,
                    customRows: form.customRows.map((r) =>
                      r.id === row.id ? { ...r, key: e.target.value } : r,
                    ),
                  })
                }
              />
              <input
                aria-label={`自訂欄位 ${index + 1} 值`}
                placeholder="欄位值，例如：繁體中文"
                maxLength={2000}
                value={row.value}
                onChange={(e) =>
                  onChange({
                    ...form,
                    customRows: form.customRows.map((r) =>
                      r.id === row.id ? { ...r, value: e.target.value } : r,
                    ),
                  })
                }
              />
              <Button
                type="button"
                variant="ghost"
                size="sm"
                aria-label={`移除自訂欄位 ${index + 1}`}
                disabled={busy}
                onClick={() =>
                  onChange({
                    ...form,
                    customRows: form.customRows.filter((r) => r.id !== row.id),
                  })
                }
              >
                <X size={14} />
              </Button>
            </div>
          ))}
        </fieldset>
        <div className="metadata-editor-footer">
          <small>使用頁面下方的「儲存變更」一併保存文件資料與切片。</small>
          {draft && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={busy}
              onClick={onReset}
            >
              還原文件資料
            </Button>
          )}
        </div>
      </div>
    </details>
  );
}

export function MetadataView({ metadata }: { metadata: DocumentMetadata }) {
  const values = Object.entries(metadata.custom || {});
  if (
    !fields.some(([key]) => metadata[key]) &&
    !metadata.tags?.length &&
    !values.length
  )
    return null;
  return (
    <details className="result-metadata">
      <summary>
        文件資料{metadata.source ? ` · ${metadata.source}` : ""}
      </summary>
      <dl>
        {fields.map(([key, label]) =>
          metadata[key] ? (
            <div key={key}>
              <dt>{label}</dt>
              <dd>
                {key === "source_url" ? (
                  <a
                    href={metadata[key]}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {metadata[key]}
                  </a>
                ) : (
                  metadata[key]
                )}
              </dd>
            </div>
          ) : null,
        )}
        {!!metadata.tags?.length && (
          <div>
            <dt>標籤</dt>
            <dd>{metadata.tags.join("、")}</dd>
          </div>
        )}
        {values.map(([key, value]) => (
          <div key={key}>
            <dt>{key}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </details>
  );
}
