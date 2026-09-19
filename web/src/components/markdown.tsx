"use client";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Components, ExtraProps } from "react-markdown";
import { createElement, memo, type ReactNode } from "react";
export const Markdown = memo(function Markdown({
  children,
  source = false,
  highlightLine,
}: {
  children: string;
  source?: boolean;
  highlightLine?: number;
}) {
  const components: Components = {
    img: ({ alt }) => <span className="muted">[圖片：{alt || "未命名"}]</span>,
    a: ({ children, href }) => (
      <a href={href} target="_blank" rel="noreferrer">
        {children}
      </a>
    ),
  };
  if (source)
    Object.assign(
      components,
      Object.fromEntries(
        [
          "p",
          "h1",
          "h2",
          "h3",
          "h4",
          "h5",
          "h6",
          "pre",
          "table",
          "ul",
          "ol",
          "blockquote",
        ].map((tag) => [
          tag,
          ({ node, children }: ExtraProps & { children?: ReactNode }) =>
            createElement(
              tag,
              {
                "data-source-line": node?.position?.start.line,
                className:
                  node?.position?.start.line === highlightLine
                    ? "source-highlight"
                    : undefined,
              },
              children,
            ),
        ]),
      ),
    );
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {children}
      </ReactMarkdown>
    </div>
  );
});
