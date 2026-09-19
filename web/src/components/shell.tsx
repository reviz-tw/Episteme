"use client";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import {
  Database,
  LayoutDashboard,
  Search,
  LogOut,
  ArrowUpRight,
} from "lucide-react";
import { useEffect } from "react";
import { api, post } from "@/lib/api";
export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname(),
    router = useRouter();
  const me = useQuery({
    queryKey: ["me"],
    queryFn: () =>
      api<{
        username: string;
        authenticated: boolean;
        setup_required: boolean;
      }>("/auth/me"),
  });
  useEffect(() => {
    if (me.data?.setup_required) router.replace("/setup");
    else if (me.data && !me.data.authenticated) router.replace("/login");
  }, [me.data, router]);
  if (!me.data?.authenticated)
    return (
      <div className="loading-screen">
        {me.error ? `無法連線：${me.error.message}` : "正在開啟知識工作台…"}
      </div>
    );
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Link className="brand" href="/dashboard">
          <span className="brand-mark">
            <i />
            <i />
            <i />
            <i />
          </span>
          episteme<span className="brand-dot">.</span>
        </Link>
        <div className="workspace-label">KNOWLEDGE WORKSPACE</div>
        <nav>
          <Link
            className={
              path.startsWith("/dashboard") || path.startsWith("/documents")
                ? "active"
                : ""
            }
            href="/dashboard"
          >
            <LayoutDashboard size={18} />
            知識庫總覽<span>01</span>
          </Link>
          <Link
            className={path === "/playground" ? "active" : ""}
            href="/playground"
          >
            <Search size={18} />
            檢索調試場<span>02</span>
          </Link>
        </nav>
        <div className="sidebar-note">
          <Database size={22} />
          <p>
            讓知識，
            <br />
            經得起追問。
          </p>
          <small>整理原文・人工審核・可追溯檢索</small>
          <ArrowUpRight size={18} className="note-arrow" />
        </div>
        <div className="account">
          <span className="avatar">{me.data.username[0].toUpperCase()}</span>
          <div>
            <strong>{me.data.username}</strong>
            <small>工作台管理員</small>
          </div>
          <button
            aria-label="登出"
            onClick={async () => {
              await post("/auth/logout");
              router.replace("/login");
            }}
          >
            <LogOut size={17} />
          </button>
        </div>
      </aside>
      <main className="main">
        <header className="topbar">
          <span>
            工作空間 <span className="slash">/</span>{" "}
            {path === "/playground"
              ? "檢索調試場"
              : path.startsWith("/documents")
                ? "文件審核"
                : "知識庫總覽"}
          </span>
          <span className="private-label">
            <span className="dot" />
            私人知識庫
          </span>
        </header>
        {children}
      </main>
    </div>
  );
}
