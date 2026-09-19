"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowRight, BookOpen } from "lucide-react";
import { api, post } from "@/lib/api";
import { Button } from "@/components/ui/button";
export function AuthForm({ setup = false }: { setup?: boolean }) {
  const router = useRouter(),
    qc = useQueryClient();
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    api<{ setup_required: boolean; authenticated: boolean }>("/auth/me")
      .then((me) => {
        if (me.authenticated) router.replace("/dashboard");
        else if (me.setup_required && !setup) router.replace("/setup");
        else if (!me.setup_required && setup) router.replace("/login");
      })
      .catch((e) => setError(e.message));
  }, [setup, router]);
  return (
    <div className="auth-page">
      <div className="auth-story">
        <Link className="brand" href="/">
          episteme.
        </Link>
        <div>
          <span className="eyebrow">FROM INFORMATION TO UNDERSTANDING</span>
          <h1>
            好的答案，
            <br />
            從值得信任的
            <br />
            <em>知識開始。</em>
          </h1>
          <p>
            將零散文件整理成可檢索的知識。
            <br />
            每一個切片，都經過你的確認。
          </p>
        </div>
        <small>你的資料，你的知識工作台。</small>
      </div>
      <div className="auth-panel">
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError("");
            const data = new FormData(e.currentTarget),
              values = {
                username: String(data.get("username")),
                password: String(data.get("password")),
              };
            try {
              if (setup) await post("/auth/setup", values);
              await post("/auth/login", values);
              await qc.invalidateQueries({ queryKey: ["me"] });
              router.push("/dashboard");
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <BookOpen size={32} />
          <span className="eyebrow">
            {setup ? "WELCOME TO EPISTEME" : "YOUR KNOWLEDGE, CONNECTED"}
          </span>
          <h2>{setup ? "建立你的知識工作台" : "歡迎回來"}</h2>
          <p className="muted">
            {setup
              ? "第一次使用，先建立管理員帳號。"
              : "登入後，繼續整理與探索你的知識。"}
          </p>
          <label>
            管理員帳號
            <input
              name="username"
              required
              minLength={3}
              maxLength={50}
              autoComplete="username"
              placeholder="輸入帳號"
            />
          </label>
          <label>
            密碼
            <input
              type="password"
              name="password"
              required
              minLength={setup ? 12 : 1}
              maxLength={1024}
              autoComplete={setup ? "new-password" : "current-password"}
              placeholder={setup ? "至少 12 個字元" : "輸入密碼"}
            />
          </label>
          {error && (
            <div className="notice error" role="alert">
              {error}
            </div>
          )}
          <Button disabled={busy} type="submit">
            {busy ? "處理中…" : setup ? "建立工作台" : "登入工作台"}
            <ArrowRight size={17} />
          </Button>
          <small className="muted">所有文件保留在你部署的服務中。</small>
        </form>
      </div>
    </div>
  );
}
