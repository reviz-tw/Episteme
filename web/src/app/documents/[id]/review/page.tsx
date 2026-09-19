"use client";
import { use, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  ArrowLeft,
  Download,
  Save,
  CheckCheck,
  ArrowUpRight,
  RotateCw,
  FileText,
  Layers,
} from "lucide-react";
import { Shell } from "@/components/shell";
import { Button } from "@/components/ui/button";
import { Markdown } from "@/components/markdown";
import { ChunkCard } from "@/components/chunk/card";
import { Jobs } from "@/components/jobs";
import {
  MetadataEditor,
  MetadataForm,
  metadataFromForm,
} from "@/components/document-metadata";
import {
  api,
  post,
  API,
  Doc,
  Chunk,
  statusLabel,
  estimateTokens,
} from "@/lib/api";
export default function ReviewPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <Shell>
      <Review id={id} />
    </Shell>
  );
}
function Review({ id }: { id: string }) {
  const qc = useQueryClient(),
    list = useRef<HTMLDivElement>(null),
    original = useRef<HTMLDivElement>(null);
  const [edits, setEdits] = useState<Record<string, Chunk>>({}),
    [metadataDraft, setMetadataDraft] = useState<{
      revision: number;
      form: MetadataForm;
    } | null>(null),
    [selected, setSelected] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [message, setMessage] = useState(""),
    [size, setSize] = useState(512),
    [mobilePanel, setMobilePanel] = useState<"source" | "chunks">("chunks"),
    [highlightLine, setHighlightLine] = useState<number>();
  const doc = useQuery({
      queryKey: ["document", id],
      queryFn: () => api<Doc>(`/documents/${id}`),
      refetchInterval:
        busy || Object.keys(edits).length || metadataDraft ? false : 5000,
    }),
    chunks = useQuery({
      queryKey: ["chunks", id],
      queryFn: () => api<Chunk[]>(`/documents/${id}/chunks`),
    });
  const rows = (chunks.data || []).map((c) => edits[c.id] || c),
    count = Object.keys(edits).length,
    hasChanges = count > 0 || metadataDraft !== null,
    metadataPending = ["queued", "running"].includes(
      doc.data?.metadata_generation?.status || "",
    ),
    reviewed = rows.filter((c) => c.is_reviewed || c.is_excluded).length;
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => list.current,
    estimateSize: () => 350,
    overscan: 3,
    getItemKey: (i) => rows[i].id,
  });
  useEffect(() => {
    if (!hasChanges) return;
    const handler = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [hasChanges]);
  useEffect(() => {
    if (doc.data) setSize(doc.data.chunk_size);
  }, [doc.data?.chunk_size]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!hasChanges && !busy)
      void qc.invalidateQueries({ queryKey: ["chunks", id] });
  }, [doc.data?.revision]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const target = new URLSearchParams(window.location.search).get("chunk");
    if (!target || !chunks.data) return;
    const i = chunks.data.findIndex((c) => c.id === target);
    if (i >= 0) {
      setSelected(target);
      virtual.scrollToIndex(i, { align: "center" });
    }
  }, [chunks.data]); // eslint-disable-line react-hooks/exhaustive-deps
  const select = (c: Chunk) => {
    setSelected(c.id);
    if (!doc.data) return;
    const prefix = new TextDecoder().decode(
      new TextEncoder()
        .encode(doc.data.source_markdown)
        .slice(0, c.source_start),
    );
    const line = prefix.split("\n").length;
    const elements = Array.from(
      original.current?.querySelectorAll<HTMLElement>("[data-source-line]") ||
        [],
    );
    let target = elements[0];
    for (const element of elements) {
      if (Number(element.dataset.sourceLine) <= line) target = element;
    }
    if (target) setHighlightLine(Number(target.dataset.sourceLine));
  };
  useEffect(() => {
    const container = original.current;
    const target = container?.querySelector<HTMLElement>(
      `[data-source-line="${highlightLine}"]`,
    );
    if (container && target)
      container.scrollTo({
        top:
          target.getBoundingClientRect().top -
          container.getBoundingClientRect().top +
          container.scrollTop -
          25,
        behavior: "smooth",
      });
  }, [highlightLine, mobilePanel]);
  const save = async (): Promise<Chunk[]> => {
    if (!hasChanges) return chunks.data || [];
    const result = await api<Chunk[]>(`/documents/${id}/chunks`, {
      method: "PATCH",
      body: JSON.stringify({
        ...(metadataDraft
          ? {
              document_revision: metadataDraft.revision,
              document_metadata: metadataFromForm(metadataDraft.form),
            }
          : {}),
        chunks: Object.values(edits).map((c) => ({
          id: c.id,
          revision: c.revision,
          raw_markdown: c.raw_markdown,
          breadcrumbs: c.breadcrumbs,
          faq: c.metadata.faq?.map((s) => s.trim()).filter(Boolean) || [],
          is_reviewed: c.is_reviewed,
          is_excluded: c.is_excluded,
        })),
      }),
    });
    qc.setQueryData(["chunks", id], result);
    setEdits({});
    setMetadataDraft(null);
    return result;
  };
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await fn();
      await qc.invalidateQueries({ queryKey: ["document", id] });
      await qc.invalidateQueries({ queryKey: ["jobs"] });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const structure = (
    c: Chunk,
    action: "split" | "merge" | "reindex",
    position?: number,
  ) =>
    void run(async () => {
      const fresh = await save(),
        current = fresh.find((x) => x.id === c.id)!;
      const next = fresh[fresh.findIndex((x) => x.id === c.id) + 1];
      await post(
        `/chunks/${c.id}/${action}`,
        action === "reindex"
          ? {}
          : {
              revision: current.revision,
              ...(action === "split"
                ? { position }
                : { next_revision: next?.revision }),
            },
      );
      await chunks.refetch();
      setMessage(
        action === "reindex"
          ? "局部索引作業已加入佇列。"
          : "切片結構已儲存，請重新確認內容。",
      );
    });
  if (doc.error || chunks.error)
    return (
      <div className="page notice error">
        {doc.error?.message || chunks.error?.message}
      </div>
    );
  if (!doc.data || !chunks.data)
    return <div className="page">正在載入文件…</div>;
  return (
    <div className="page review-page">
      <Link
        className="back-link"
        href="/dashboard"
        onClick={(e) => {
          if (hasChanges && !confirm("有尚未儲存的變更，要離開嗎？"))
            e.preventDefault();
        }}
      >
        <ArrowLeft size={15} />
        回到文件庫
      </Link>
      <div className="page-heading">
        <div>
          <span className="eyebrow">REVIEW & REFINE</span>
          <h1 className="document-title">{doc.data.filename}</h1>
          <p className="muted">
            <span className={`badge ${doc.data.status}`}>
              {statusLabel[doc.data.status]}
            </span>{" "}
            {rows.length} 個切片 · {doc.data.total_tokens.toLocaleString()} 估算
            tokens
          </p>
        </div>
        <a
          className="button button-outline"
          href={`${API}/documents/${id}/original`}
        >
          <Download size={15} />
          下載原檔
        </a>
      </div>
      {(error || message) && (
        <div
          role={error ? "alert" : "status"}
          className={`notice ${error ? "error" : "success"}`}
        >
          {error || message}
        </div>
      )}
      <MetadataEditor
        metadata={doc.data.metadata || {}}
        draft={metadataDraft?.form}
        busy={busy}
        onChange={(form) =>
          setMetadataDraft((previous) => ({
            revision: previous?.revision ?? doc.data!.revision,
            form,
          }))
        }
        onReset={() => setMetadataDraft(null)}
        generation={doc.data.metadata_generation}
        generateDisabled={hasChanges}
        onGenerate={() =>
          void run(async () => {
            await post(`/documents/${id}/metadata/extract`);
            setMessage("已排程自動擷取；完成後會補齊空白欄位，請再確認。");
          })
        }
      />
      <div className="review-toolbar">
        <span>先校對內容，再確認入庫。</span>
        <div className="row-actions">
          <label>
            切片長度{" "}
            <input
              aria-label="切片長度"
              type="number"
              min={32}
              max={4096}
              value={size}
              onChange={(e) => setSize(Number(e.target.value))}
              disabled={busy}
            />
          </label>
          <Button
            variant="outline"
            size="sm"
            disabled={busy || metadataDraft !== null}
            onClick={() => {
              if (
                !confirm(
                  "重新切分會清除人工修改與審核記錄，並排程刪除舊向量。確定繼續？",
                )
              )
                return;
              void run(async () => {
                const latest = await api<Doc>(`/documents/${id}`);
                await post(`/documents/${id}/rechunk`, {
                  chunk_size: size,
                  revision: latest.revision,
                  confirm: true,
                });
                setEdits({});
                await chunks.refetch();
                setMessage("已重新切分，請再次審核。");
              });
            }}
          >
            <RotateCw size={13} />
            重新切分
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() =>
              setEdits(
                Object.fromEntries(
                  rows.map((c) => [c.id, { ...c, is_reviewed: true }]),
                ),
              )
            }
          >
            <CheckCheck size={14} />
            全部確認
          </Button>
        </div>
      </div>
      <div className="mobile-review-tabs">
        <button
          className={mobilePanel === "source" ? "selected" : ""}
          onClick={() => setMobilePanel("source")}
        >
          原始文件
        </button>
        <button
          className={mobilePanel === "chunks" ? "selected" : ""}
          onClick={() => setMobilePanel("chunks")}
        >
          切片審核
        </button>
      </div>
      <div className={`review-grid show-${mobilePanel}`}>
        <section className="source-panel">
          <div className="panel-heading">
            <span>
              <FileText size={16} />
              原始文件
            </span>
            <small>
              {doc.data.mime_type === "application/json"
                ? "JSON 結構化檢視 · 原始 JSON 可下載"
                : "點選切片，定位原始段落"}
            </small>
          </div>
          <div className="source-scroll" ref={original}>
            <Markdown source highlightLine={highlightLine}>
              {doc.data.source_markdown}
            </Markdown>
          </div>
        </section>
        <section className="chunks-panel">
          <div className="panel-heading">
            <span>
              <Layers size={16} />
              切片審核
            </span>
            <small>
              {reviewed} / {rows.length} 已確認
            </small>
          </div>
          <div className="chunks-scroll" ref={list}>
            <div
              style={{
                height: virtual.getTotalSize(),
                position: "relative",
                width: "100%",
              }}
            >
              {virtual.getVirtualItems().map((item) => {
                const c = rows[item.index];
                return (
                  <div
                    key={c.id}
                    data-index={item.index}
                    ref={virtual.measureElement}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      transform: `translateY(${item.start}px)`,
                      paddingBottom: 14,
                    }}
                  >
                    <ChunkCard
                      chunk={c}
                      dirty={!!edits[c.id]}
                      limit={doc.data!.chunk_size}
                      selected={selected === c.id}
                      busy={busy}
                      hasNext={item.index < rows.length - 1}
                      onSelect={() => select(c)}
                      onChange={(value) => {
                        value.token_count = estimateTokens(value);
                        setEdits((prev) => ({ ...prev, [c.id]: value }));
                      }}
                      onSplit={(position) => structure(c, "split", position)}
                      onMerge={() => structure(c, "merge")}
                      onIndex={() => structure(c, "reindex")}
                    />
                  </div>
                );
              })}
            </div>
          </div>
        </section>
      </div>
      <div className="review-bottom">
        <div>
          <strong>
            {rows.length ? Math.round((reviewed / rows.length) * 100) : 0}%
            已確認
          </strong>
          <small>
            {hasChanges
              ? [
                  count ? `${count} 個切片` : "",
                  metadataDraft ? "文件資料" : "",
                ]
                  .filter(Boolean)
                  .join("、") + "有未儲存變更"
              : "草稿已儲存"}{" "}
            · 確認前不會進入檢索
          </small>
        </div>
        <div className="row-actions">
          <Button
            variant="outline"
            disabled={busy || !hasChanges}
            onClick={() =>
              void run(async () => {
                await save();
                setMessage("草稿已儲存。");
              })
            }
          >
            <Save size={15} />
            儲存變更
          </Button>
          <Button
            disabled={
              busy ||
              metadataPending ||
              !rows.length ||
              reviewed !== rows.length
            }
            onClick={() =>
              void run(async () => {
                await save();
                await post(`/documents/${id}/commit`);
                setMessage("已送出索引作業。完成後可至檢索調試場驗證結果。");
              })
            }
          >
            {busy ? "處理中…" : "核可並入庫"}
            <ArrowUpRight size={16} />
          </Button>
        </div>
      </div>
      <Jobs />
    </div>
  );
}
