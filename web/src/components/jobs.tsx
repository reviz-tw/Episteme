"use client";
import { useState } from "react";
import { Pause, Play, RefreshCw, X } from "lucide-react";
import { useJobs } from "@/hooks/use-jobs";
import { post, statusLabel } from "@/lib/api";
import { Button } from "./ui/button";
export function Jobs() {
  const jobs = useJobs(),
    [error, setError] = useState(""),
    [busy, setBusy] = useState("");
  const rows = jobs.data?.slice(0, 6) || [];
  if (!rows.length) return null;
  return (
    <section className="jobs-panel">
      <div className="section-heading">
        <h2>索引作業</h2>
        <span className="eyebrow">LIVE PROGRESS</span>
      </div>
      {error && (
        <p role="alert" className="notice error">
          {error}
        </p>
      )}
      {rows.map((job) => (
        <div className="job-row" key={job.id}>
          <RefreshCw
            size={16}
            className={job.status === "running" ? "spin" : ""}
          />
          <div className="job-main">
            <div>
              <strong>
                {{
                  commit: "文件入庫",
                  rebuild: "全庫重建",
                  exclude: "排除切片",
                  delete: "刪除文件",
                  merge_cleanup: "合併清理",
                  rechunk_cleanup: "重切清理",
                }[job.kind] || job.kind}
              </strong>
              <small>
                {job.completed} / {job.total} chunks
              </small>
            </div>
            <progress max={Math.max(job.total, 1)} value={job.completed} />
            {job.error && <p className="error-text">{job.error}</p>}
          </div>
          <span className={`badge ${job.status}`}>
            {statusLabel[job.status]}
          </span>
          {!["completed", "cancelled"].includes(job.status) && (
            <div className="row-actions">
              {[
                job.status === "paused" || job.status === "failed"
                  ? "resume"
                  : "pause",
                "cancel",
              ].map((action) => (
                <Button
                  key={action}
                  size="sm"
                  variant="ghost"
                  disabled={busy === job.id}
                  aria-label={
                    action === "resume"
                      ? "繼續作業"
                      : action === "pause"
                        ? "暫停作業"
                        : "取消作業"
                  }
                  onClick={async () => {
                    setBusy(job.id);
                    setError("");
                    try {
                      await post(`/indexing/jobs/${job.id}`, { action });
                      await jobs.refetch();
                    } catch (e) {
                      setError((e as Error).message);
                    } finally {
                      setBusy("");
                    }
                  }}
                >
                  {action === "resume" ? (
                    <Play size={14} />
                  ) : action === "pause" ? (
                    <Pause size={14} />
                  ) : (
                    <X size={14} />
                  )}
                </Button>
              ))}
            </div>
          )}
        </div>
      ))}
    </section>
  );
}
