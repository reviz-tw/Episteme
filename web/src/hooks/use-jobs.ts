"use client";
import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, Job, API } from "@/lib/api";
export function useJobs() {
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["jobs"],
    queryFn: () => api<Job[]>("/indexing/jobs"),
    refetchInterval: 10000,
  });
  useEffect(() => {
    if (!query.data) return;
    // Refresh published state after either an SSE event or the polling fallback.
    void qc.invalidateQueries({ queryKey: ["documents"] });
    void qc.invalidateQueries({ queryKey: ["document"] });
    void qc.invalidateQueries({ queryKey: ["chunks"] });
    void qc.invalidateQueries({ queryKey: ["dashboard"] });
  }, [qc, query.data]);
  useEffect(() => {
    const events = new EventSource(API + "/indexing/stream");
    let previous = "";
    events.addEventListener("progress", (e) => {
      const raw = (e as MessageEvent).data;
      if (raw === previous) return;
      previous = raw;
      try {
        const jobs = JSON.parse(raw) as Job[];
        qc.setQueryData(["jobs"], jobs);
      } catch {
        /* The periodic query remains the reconnect fallback. */
      }
    });
    return () => events.close();
  }, [qc]);
  return query;
}
