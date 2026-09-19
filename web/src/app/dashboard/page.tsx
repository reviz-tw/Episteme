"use client";
import { useRef, useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUpRight,
  FileText,
  Layers,
  Clock3,
  HardDrive,
  Upload,
  Plus,
  Search,
  Trash2,
  RefreshCw,
  ArrowRight,
} from "lucide-react";
import { Shell } from "@/components/shell";
import { Button } from "@/components/ui/button";
import { Jobs } from "@/components/jobs";
import { api, post, API, Doc, Dashboard, statusLabel } from "@/lib/api";
export default function DashboardPage() {
  return (
    <Shell>
      <DashboardContent />
    </Shell>
  );
}
function DashboardContent() {
  const qc = useQueryClient(),
    input = useRef<HTMLInputElement>(null);
  const [filter, setFilter] = useState(""),
    [stateFilter, setStateFilter] = useState("all"),
    [selected, setSelected] = useState<string[]>([]),
    [busy, setBusy] = useState(false),
    [progress, setProgress] = useState<number | null>(null),
    [error, setError] = useState(""),
    [message, setMessage] = useState(""),
    [drag, setDrag] = useState(false);
  const docs = useQuery({
      queryKey: ["documents"],
      queryFn: () => api<Doc[]>("/documents"),
      refetchInterval: (query) =>
        query.state.data?.some((d) =>
          ["queued", "running"].includes(d.metadata_generation?.status || ""),
        )
          ? 3000
          : false,
    }),
    stats = useQuery({
      queryKey: ["dashboard"],
      queryFn: () => api<Dashboard>("/dashboard"),
      refetchInterval: 30000,
    });
  const refresh = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: ["documents"] }),
      qc.invalidateQueries({ queryKey: ["dashboard"] }),
      qc.invalidateQueries({ queryKey: ["jobs"] }),
    ]);
  };
  const upload = async (files: FileList | null) => {
    if (!files?.length || busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      for (const file of Array.from(files)) {
        setProgress(0);
        await new Promise<void>((resolve, reject) => {
          const xhr = new XMLHttpRequest();
          xhr.open("POST", API + "/documents/upload");
          xhr.upload.onprogress = (e) => {
            if (e.lengthComputable)
              setProgress(Math.round((e.loaded / e.total) * 100));
          };
          xhr.onerror = () => reject(new Error("網路連線失敗"));
          xhr.onload = () => {
            let body;
            try {
              body = JSON.parse(xhr.responseText);
            } catch {
              reject(new Error("伺服器回應格式錯誤"));
              return;
            }
            if (xhr.status >= 200 && xhr.status < 300) resolve();
            else reject(new Error(body.error || "上傳失敗"));
          };
          const data = new FormData();
          data.append("file", file);
          xhr.send(data);
        });
      }
      setMessage(`已上傳 ${files.length} 份文件，請開啟文件進行審核。`);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
      setProgress(null);
      if (input.current) input.current.value = "";
      await refresh();
    }
  };
  const rows =
    docs.data?.filter(
      (d) =>
        d.filename.toLowerCase().includes(filter.toLowerCase()) &&
        (stateFilter === "all" || d.status === stateFilter),
    ) || [];
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="page">
      <div className="page-heading">
        <div>
          <span className="eyebrow">YOUR KNOWLEDGE, IN FOCUS</span>
          <h1>
            知識庫總覽<span className="heading-dot">.</span>
          </h1>
          <p className="muted">每一份文件，都是下一個好答案的起點。</p>
        </div>
        <Button onClick={() => input.current?.click()} disabled={busy}>
          <Plus size={17} />
          新增文件
        </Button>
      </div>
      <input
        ref={input}
        type="file"
        accept=".md,.markdown,.pdf,.json"
        multiple
        hidden
        onChange={(e) => void upload(e.target.files)}
      />
      {(error || docs.error || stats.error) && (
        <div className="notice error" role="alert">
          {error || docs.error?.message || stats.error?.message}
        </div>
      )}
      {message && (
        <div className="notice success" role="status">
          {message}
        </div>
      )}
      <div className="stats-grid">
        {[
          {
            label: "知識文件",
            value: stats.data?.documents,
            icon: FileText,
            unit: "份文件",
          },
          {
            label: "已索引切片",
            value: stats.data?.indexed_chunks,
            icon: Layers,
            unit: "個 chunks",
          },
          {
            label: "等待審核",
            value: stats.data?.draft_chunks,
            icon: Clock3,
            unit: "個 chunks",
            warm: true,
          },
          {
            label: "向量容量估算",
            value: stats.data
              ? (stats.data.vector_bytes_estimate / 1048576).toFixed(1)
              : undefined,
            icon: HardDrive,
            unit: "MB · dense 原始資料",
          },
        ].map((card) => (
          <div
            className={`stat-card ${card.warm ? "warm" : ""}`}
            key={card.label}
          >
            <div>
              <span>{card.label}</span>
              <card.icon size={19} />
            </div>
            <strong>{card.value ?? "—"}</strong>
            <small>{card.unit}</small>
          </div>
        ))}
      </div>
      <div className="service-strip">
        <span className="eyebrow">SYSTEM STATUS</span>
        {[
          ["embedding", "Embedding"],
          ["rerank", "Reranker"],
          ["qdrant", "Qdrant"],
          ...(stats.data?.metadata_enabled
            ? [["metadata", "Metadata AI"]]
            : []),
        ].map(([key, name]) => (
          <span key={key}>
            <i className={`dot ${stats.data?.health[key] ? "" : "offline"}`} />
            {name}
            <small>
              {!stats.data
                ? "檢查中"
                : stats.data.health[key]
                  ? "正常"
                  : "未連線"}
            </small>
          </span>
        ))}
        <span className="model-label">
          {stats.data?.model || "—"}
          <ArrowUpRight size={14} />
        </span>
      </div>
      <button
        className={`upload-zone ${drag ? "drag" : ""}`}
        disabled={busy}
        onDragOver={(e) => {
          e.preventDefault();
          setDrag(true);
        }}
        onDragLeave={() => setDrag(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDrag(false);
          void upload(e.dataTransfer.files);
        }}
        onClick={() => input.current?.click()}
      >
        <span className="upload-icon">
          <Upload size={22} />
        </span>
        <div>
          <strong>
            {progress === null
              ? "把文件放進來，開始整理知識"
              : progress < 100
                ? `正在上傳 ${progress}%`
                : "正在解析文件與建立切片…"}
          </strong>
          <span>
            拖曳檔案至此，或點擊選擇 · Markdown / PDF / JSON · 每份上限 20 MB
          </span>
          {progress !== null && <progress value={progress} max={100} />}
        </div>
        <ArrowRight size={20} />
      </button>
      <section className="library">
        <div className="section-heading">
          <div className="title-with-count">
            <h2>文件庫</h2>
            <span>{docs.data?.length || 0}</span>
          </div>
          <div className="row-actions">
            <Button
              variant="outline"
              size="sm"
              disabled={busy || !docs.data?.length}
              onClick={() => {
                if (
                  confirm(
                    "依目前模型重建所有已審核切片？未審核切片會阻止提交。",
                  )
                )
                  void run(() => post("/indexing/rebuild"));
              }}
            >
              <RefreshCw size={14} />
              全庫重建
            </Button>
            {selected.length > 0 && (
              <Button
                variant="destructive"
                size="sm"
                disabled={busy}
                onClick={() => {
                  if (confirm(`刪除 ${selected.length} 份文件及其所有切片？`))
                    void run(async () => {
                      for (const id of selected)
                        await api(`/documents/${id}`, { method: "DELETE" });
                      setSelected([]);
                    });
                }}
              >
                <Trash2 size={14} />
                刪除 {selected.length} 份
              </Button>
            )}
          </div>
        </div>
        <div className="table-tools">
          <div className="tabs">
            {[
              ["all", "全部文件"],
              ["draft", "待審核"],
              ["indexed", "已索引"],
              ["stale", "待更新"],
            ].map(([key, label]) => (
              <button
                className={stateFilter === key ? "selected" : ""}
                onClick={() => setStateFilter(key)}
                key={key}
              >
                {label}
              </button>
            ))}
          </div>
          <label className="search-input">
            <Search size={16} />
            <input
              aria-label="搜尋文件"
              placeholder="搜尋文件名稱…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </label>
        </div>
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>
                  <input
                    type="checkbox"
                    aria-label="選取所有顯示文件"
                    checked={
                      rows.length > 0 &&
                      rows.every((d) => selected.includes(d.id))
                    }
                    onChange={(e) =>
                      setSelected(e.target.checked ? rows.map((d) => d.id) : [])
                    }
                  />
                </th>
                <th>文件名稱</th>
                <th>切片 / Tokens</th>
                <th>審核進度</th>
                <th>狀態</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((d) => (
                <tr key={d.id}>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`選取 ${d.filename}`}
                      checked={selected.includes(d.id)}
                      onChange={(e) =>
                        setSelected(
                          e.target.checked
                            ? [...selected, d.id]
                            : selected.filter((id) => id !== d.id),
                        )
                      }
                    />
                  </td>
                  <td>
                    <Link
                      className="file-cell"
                      href={`/documents/${d.id}/review`}
                    >
                      <span className="file-icon">
                        <FileText size={20} />
                      </span>
                      <div>
                        <strong>{d.filename}</strong>
                        <small>
                          {new Date(d.updated_at).toLocaleDateString("zh-TW")} ·{" "}
                          {d.mime_type === "application/pdf"
                            ? "PDF"
                            : d.mime_type === "application/json"
                              ? "JSON"
                              : "MARKDOWN"}
                        </small>
                      </div>
                    </Link>
                  </td>
                  <td>
                    <strong>{d.chunk_count}</strong>
                    <small className="block">
                      {d.total_tokens.toLocaleString()} tokens
                    </small>
                  </td>
                  <td>
                    <div className="review-progress">
                      <span>
                        {d.reviewed_count} / {d.chunk_count}
                        <small>已確認</small>
                      </span>
                      <progress
                        value={d.reviewed_count}
                        max={Math.max(d.chunk_count, 1)}
                      />
                    </div>
                  </td>
                  <td>
                    <span className={`badge ${d.status}`}>
                      {statusLabel[d.status] || d.status}
                    </span>
                    {d.metadata_generation && (
                      <small className="block">
                        {
                          {
                            queued: "AI 排程中",
                            running: "AI 擷取中",
                            completed: "AI 已預填",
                            failed: "AI 擷取失敗",
                            skipped: "已保留人工修改",
                          }[d.metadata_generation.status]
                        }
                      </small>
                    )}
                  </td>
                  <td>
                    <Link
                      className="table-link"
                      href={`/documents/${d.id}/review`}
                    >
                      開啟
                      <ArrowUpRight size={15} />
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {!rows.length && (
          <div className="empty-state">
            <Layers size={30} />
            <h3>
              {docs.isLoading
                ? "正在讀取文件…"
                : docs.data?.length
                  ? "找不到符合的文件"
                  : "你的知識庫，從第一份文件開始"}
            </h3>
            <p>
              {docs.data?.length
                ? "試試其他關鍵字或狀態。"
                : "上傳 Markdown、PDF 或 JSON，系統會自動切分，交由你確認內容。"}
            </p>
          </div>
        )}
      </section>
      <Jobs />
      <footer className="page-footer">
        EPISTEME <span>知識的品質，來自每一次確認。</span>
      </footer>
    </div>
  );
}
