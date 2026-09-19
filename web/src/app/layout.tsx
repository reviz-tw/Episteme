import type { Metadata } from "next";
import { Providers } from "./providers";
import "./globals.css";
export const metadata: Metadata = {
  title: "Episteme — 知識工作台",
  description: "從原始文件，到值得信任的知識。審核、整理與檢索你的知識庫。",
};
export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="zh-Hant">
      <body>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
