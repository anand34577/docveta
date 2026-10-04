import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * A small Markdown renderer for AI answers: paragraphs, headings, lists, bold/italic, code,
 * tables, quotes and links. It builds React elements (never HTML strings), so model output
 * can't inject markup. `inline` lets callers turn extra patterns (citation markers) into
 * elements inside any text run.
 */
export function Markdown({ text, inline, className }: { text: string; inline?: (s: string, key: string) => React.ReactNode[]; className?: string }) {
  const blocks = React.useMemo(() => parseBlocks(text), [text]);
  const ctx = { inline: inline ?? ((s: string) => [s]) };
  return <div className={cn("kz-md space-y-3 break-words text-[15px] leading-relaxed", className)}>{blocks.map((b, i) => renderBlock(b, String(i), ctx))}</div>;
}

type Block =
  | { t: "p"; text: string }
  | { t: "h"; level: number; text: string }
  | { t: "code"; text: string }
  | { t: "quote"; text: string }
  | { t: "list"; ordered: boolean; start: number; items: string[] }
  | { t: "table"; head: string[]; rows: string[][] }
  | { t: "hr" };

const listRe = /^(\s*)([-*+]|\d{1,3}[.)])\s+(.*)$/;

function parseBlocks(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const out: Block[] = [];
  let para: string[] = [];
  const flush = () => {
    if (para.length) out.push({ t: "p", text: para.join("\n") });
    para = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^\s*```/.test(line)) {
      flush();
      const body: string[] = [];
      for (i++; i < lines.length && !/^\s*```/.test(lines[i]); i++) body.push(lines[i]);
      out.push({ t: "code", text: body.join("\n") });
      continue;
    }
    if (!line.trim()) {
      flush();
      continue;
    }
    const h = /^(#{1,4})\s+(.*)$/.exec(line);
    if (h) {
      flush();
      out.push({ t: "h", level: h[1].length, text: h[2] });
      continue;
    }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
      flush();
      out.push({ t: "hr" });
      continue;
    }
    if (/^\s*>/.test(line)) {
      flush();
      const body: string[] = [];
      for (; i < lines.length && /^\s*>/.test(lines[i]); i++) body.push(lines[i].replace(/^\s*>\s?/, ""));
      i--;
      out.push({ t: "quote", text: body.join("\n") });
      continue;
    }
    // A table: a header row, a separator row of dashes, then rows.
    if (line.includes("|") && i + 1 < lines.length && /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(lines[i + 1])) {
      flush();
      const cells = (l: string) => l.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
      const head = cells(line);
      const rows: string[][] = [];
      for (i += 2; i < lines.length && lines[i].includes("|") && lines[i].trim(); i++) rows.push(cells(lines[i]));
      i--;
      out.push({ t: "table", head, rows });
      continue;
    }
    const li = listRe.exec(line);
    if (li) {
      flush();
      const ordered = /\d/.test(li[2]);
      const items: string[] = [];
      const start = ordered ? parseInt(li[2], 10) || 1 : 1;
      for (; i < lines.length; i++) {
        const m = listRe.exec(lines[i]);
        if (m && /\d/.test(m[2]) === ordered && m[1].length < 2) items.push(m[3]);
        else if (m || (lines[i].trim() && /^\s{2,}/.test(lines[i]))) items[items.length - 1] += "\n" + lines[i].trim(); // nested or continued
        else break;
      }
      i--;
      out.push({ t: "list", ordered, start, items });
      continue;
    }
    para.push(line);
  }
  flush();
  return out;
}

interface Ctx {
  inline: (s: string, key: string) => React.ReactNode[];
}

function renderBlock(b: Block, key: string, ctx: Ctx): React.ReactNode {
  switch (b.t) {
    case "p":
      return <p key={key} className="whitespace-pre-wrap">{renderInline(b.text, key, ctx)}</p>;
    case "h": {
      const cls = b.level <= 2 ? "text-base font-semibold" : "text-[15px] font-semibold";
      return <p key={key} role="heading" aria-level={b.level + 1} className={cls}>{renderInline(b.text, key, ctx)}</p>;
    }
    case "code":
      return <pre key={key} className="overflow-x-auto rounded-md bg-surface-2 p-3 font-mono text-[13px] leading-snug scrollbar-thin">{b.text}</pre>;
    case "quote":
      return <blockquote key={key} className="whitespace-pre-wrap border-l-2 border-border-strong pl-3 text-muted">{renderInline(b.text, key, ctx)}</blockquote>;
    case "hr":
      return <hr key={key} className="border-border" />;
    case "list": {
      const L = b.ordered ? "ol" : "ul";
      return (
        <L key={key} start={b.ordered && b.start !== 1 ? b.start : undefined} className={cn("space-y-1 pl-5", b.ordered ? "list-decimal" : "list-disc")}>
          {b.items.map((it, i) => (
            <li key={i} className="whitespace-pre-wrap pl-0.5">{renderInline(it, `${key}.${i}`, ctx)}</li>
          ))}
        </L>
      );
    }
    case "table":
      return (
        <div key={key} className="overflow-x-auto rounded-md border border-border scrollbar-thin">
          <table className="w-full text-left text-[13px]">
            <thead className="bg-surface-2">
              <tr>{b.head.map((c, i) => <th key={i} className="px-3 py-2 font-semibold">{renderInline(c, `${key}.h${i}`, ctx)}</th>)}</tr>
            </thead>
            <tbody className="divide-y divide-border">
              {b.rows.map((r, i) => (
                <tr key={i}>{r.map((c, j) => <td key={j} className="px-3 py-2 align-top">{renderInline(c, `${key}.${i}.${j}`, ctx)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      );
  }
}

// Inline: `code`, **bold**, *italic* / _italic_, [text](https://…), and the caller's patterns.
const inlineRe = /(`[^`\n]+`)|(\*\*[^*\n]+?\*\*|__[^_\n]+?__)|(\*[^*\s][^*\n]*?\*|(?<![\w])_[^_\s][^_\n]*?_(?![\w]))|(\[[^\]\n]+\]\((https?:\/\/[^)\s]+)\))/g;

function renderInline(text: string, key: string, ctx: Ctx): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  let last = 0;
  let n = 0;
  for (const m of text.matchAll(inlineRe)) {
    const at = m.index ?? 0;
    if (at > last) out.push(...ctx.inline(text.slice(last, at), `${key}.t${n}`));
    const k = `${key}.m${n++}`;
    const s = m[0];
    if (m[1]) out.push(<code key={k} className="rounded bg-surface-2 px-1 py-0.5 font-mono text-[0.9em]">{s.slice(1, -1)}</code>);
    else if (m[2]) out.push(<strong key={k} className="font-semibold">{renderInline(s.slice(2, -2), k, ctx)}</strong>);
    else if (m[3]) out.push(<em key={k}>{renderInline(s.slice(1, -1), k, ctx)}</em>);
    else if (m[4]) {
      const label = s.slice(1, s.indexOf("]("));
      out.push(
        <a key={k} href={m[5]} target="_blank" rel="noopener noreferrer nofollow" className="text-accent underline underline-offset-2">
          {label}
        </a>,
      );
    }
    last = at + s.length;
  }
  if (last < text.length) out.push(...ctx.inline(text.slice(last), `${key}.t${n}`));
  return out;
}
