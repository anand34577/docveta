import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { FileText, MessageSquarePlus, SendHorizontal, Sparkles, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { useAIEnabled, useConversations } from "@/lib/queries";
import type { Citation, ConversationMessage } from "@/lib/types";
import { cn, spaceLabel } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { Button } from "@/components/ui/button";
import { NativeSelect, Textarea } from "@/components/ui/input";
import { EmptyState, Skeleton, Spinner } from "@/components/ui/misc";
import { confirm } from "@/components/ui/confirm";

interface Turn {
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  pending?: boolean;
  error?: boolean;
}

const examples = ["When does my car insurance expire?", "How much was my last electricity bill?", "What did I pay for the laptop, and where is the warranty?"];

/** Ask a question; the answer is written from your own documents and every claim cites its page. */
export function AskPage() {
  const ai = useAIEnabled();
  const me = useCurrentUser();
  const qc = useQueryClient();
  const conversations = useConversations();
  const [convId, setConvId] = React.useState<string | null>(null);
  const [turns, setTurns] = React.useState<Turn[]>([]);
  const [text, setText] = React.useState("");
  const [space, setSpace] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [loading, setLoading] = React.useState(false);
  const end = React.useRef<HTMLDivElement>(null);
  const abort = React.useRef<AbortController | null>(null);

  // Braces: newer browsers return a Promise from scrollIntoView, and React would call it as the cleanup.
  React.useEffect(() => {
    end.current?.scrollIntoView({ block: "end", behavior: "smooth" });
  }, [turns]);
  React.useEffect(() => () => abort.current?.abort(), []);

  const open = async (id: string | null) => {
    abort.current?.abort();
    setConvId(id);
    setTurns([]);
    if (!id) return;
    setLoading(true);
    try {
      const r = await api.get<{ items: ConversationMessage[] }>(`/ai/conversations/${id}/messages`);
      setTurns(r.items.map((m) => ({ role: m.role, content: m.content, citations: m.citations ?? [] })));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setLoading(false);
    }
  };

  const ask = async (question: string) => {
    const q = question.trim();
    if (!q || busy) return;
    setText("");
    setBusy(true);
    setTurns((t) => [...t, { role: "user", content: q, citations: [] }, { role: "assistant", content: "", citations: [], pending: true }]);
    const patchLast = (f: (t: Turn) => Turn) => setTurns((t) => t.map((x, i) => (i === t.length - 1 ? f(x) : x)));
    const ctl = new AbortController();
    abort.current = ctl;
    try {
      const res = await fetch("/api/v1/ai/ask", {
        method: "POST",
        credentials: "same-origin",
        signal: ctl.signal,
        headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
        body: JSON.stringify({ question: q, conversation_id: convId ?? undefined, space_ids: space ? [space] : undefined }),
      });
      if (!res.ok || !res.body) {
        const b = await res.json().catch(() => ({}) as { title?: string });
        throw new Error((b as { title?: string }).title || "Couldn't get an answer");
      }
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let i: number;
        while ((i = buf.indexOf("\n\n")) >= 0) {
          const frame = buf.slice(0, i);
          buf = buf.slice(i + 2);
          const ev = /^event: (.*)$/m.exec(frame)?.[1];
          const data = /^data: (.*)$/m.exec(frame)?.[1];
          if (!ev || data === undefined) continue;
          const v = JSON.parse(data);
          if (ev === "citations") patchLast((t) => ({ ...t, citations: v as Citation[] }));
          else if (ev === "delta") patchLast((t) => ({ ...t, content: t.content + (v as string) }));
          else if (ev === "error") patchLast((t) => ({ ...t, content: (v as { message: string }).message, error: true }));
          else if (ev === "done") setConvId((v as { conversation_id: string }).conversation_id);
        }
      }
      qc.invalidateQueries({ queryKey: ["conversations"] });
    } catch (e) {
      if ((e as Error).name !== "AbortError") patchLast((t) => ({ ...t, content: errorMessage(e), error: true }));
    } finally {
      patchLast((t) => ({ ...t, pending: false }));
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    if (!(await confirm({ title: "Delete this conversation?", confirmLabel: "Delete", destructive: true }))) return;
    await api.del(`/ai/conversations/${id}`).catch((e) => toast.error(errorMessage(e)));
    qc.invalidateQueries({ queryKey: ["conversations"] });
    if (convId === id) void open(null);
  };

  if (ai.isLoading) return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  if (!ai.data?.chat)
    return (
      <EmptyState icon={<Sparkles />} title="Ask needs an AI provider" className="min-h-[60vh]" action={me.is_admin ? <Button asChild variant="primary"><Link to="/admin/$section" params={{ section: "ai" }}>Set up AI</Link></Button> : undefined}>
        {me.is_admin ? "Connect any OpenAI-compatible service, or one running on your own computer." : "Ask your administrator to connect an AI provider."}
      </EmptyState>
    );

  return (
    <div className="flex h-[calc(100dvh-3.5rem-5rem)] lg:h-[calc(100dvh-3.5rem)]">
      <aside className="hidden w-64 shrink-0 flex-col border-r border-border bg-sidebar/50 md:flex">
        <div className="p-3">
          <Button className="w-full" onClick={() => open(null)}>
            <MessageSquarePlus /> New question
          </Button>
        </div>
        <ul className="flex-1 space-y-0.5 overflow-y-auto scrollbar-thin px-2 pb-3">
          {conversations.isLoading && <Skeleton className="m-2 h-8" />}
          {(conversations.data ?? []).map((c) => (
            <li key={c.id} className="group relative">
              <button onClick={() => open(c.id)} className={cn("w-full truncate rounded-md px-2.5 py-2 text-left text-sm hover:bg-surface-2", convId === c.id && "bg-surface font-medium shadow-sm")}>
                {c.title || "Untitled"}
              </button>
              <button onClick={() => remove(c.id)} aria-label="Delete conversation" className="absolute right-1 top-1/2 -translate-y-1/2 rounded p-1 text-subtle opacity-0 hover:bg-surface-3 hover:text-danger focus-visible:opacity-100 group-hover:opacity-100">
                <Trash2 className="size-3.5" />
              </button>
            </li>
          ))}
        </ul>
      </aside>

      <section className="flex min-w-0 flex-1 flex-col">
        <div className="flex-1 overflow-y-auto scrollbar-thin">
          <div className="mx-auto max-w-3xl space-y-6 page-x py-6">
            {loading ? (
              <Skeleton className="h-32" />
            ) : turns.length === 0 ? (
              <div className="pt-8 text-center sm:pt-16">
                <div className="mx-auto mb-4 flex size-12 items-center justify-center rounded-2xl bg-accent-soft text-accent">
                  <Sparkles className="size-6" />
                </div>
                <h1 className="text-2xl font-semibold tracking-tight">Ask your documents</h1>
                <p className="mx-auto mt-2 max-w-md text-sm text-muted">Answers come from your own documents only, and show where they were found so you can check.</p>
                <div className="mx-auto mt-6 flex max-w-lg flex-col gap-2">
                  {examples.map((e) => (
                    <button key={e} onClick={() => ask(e)} className="rounded-lg border border-border bg-surface px-4 py-2.5 text-left text-sm hover:border-border-strong hover:bg-surface-2">
                      {e}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              turns.map((t, i) => <TurnView key={i} turn={t} />)
            )}
            <div ref={end} />
          </div>
        </div>
        <form
          className="border-t border-border bg-bg/90 backdrop-blur"
          onSubmit={(e) => {
            e.preventDefault();
            void ask(text);
          }}
        >
          <div className="mx-auto flex max-w-3xl flex-col gap-2 page-x py-3">
            <div className="flex items-end gap-2 rounded-xl border border-border bg-surface p-2 focus-within:border-accent focus-within:ring-3 focus-within:ring-ring">
              <Textarea
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    void ask(text);
                  }
                }}
                rows={1}
                placeholder="Ask anything about your documents…"
                aria-label="Your question"
                className="max-h-40 min-h-9 flex-1 resize-none border-0 py-1.5 focus:ring-0"
              />
              <Button type="submit" variant="primary" size="icon" loading={busy} disabled={!text.trim()} aria-label="Ask">
                <SendHorizontal />
              </Button>
            </div>
            {me.spaces.length > 1 && (
              <label className="flex items-center gap-2 text-xs text-subtle">
                Look in
                <NativeSelect className="h-8 w-auto text-xs sm:h-8" value={space} onChange={(e) => setSpace(e.target.value)}>
                  <option value="">All my spaces</option>
                  {me.spaces.map((s) => (
                    <option key={s.id} value={s.id}>
                      {spaceLabel(s)}
                    </option>
                  ))}
                </NativeSelect>
              </label>
            )}
          </div>
        </form>
      </section>
    </div>
  );
}

function TurnView({ turn }: { turn: Turn }) {
  if (turn.role === "user")
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] whitespace-pre-wrap rounded-2xl rounded-br-md bg-accent px-4 py-2.5 text-[15px] text-accent-fg">{turn.content}</div>
      </div>
    );
  return (
    <div className="flex gap-3">
      <span className="mt-1 flex size-7 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
        <Sparkles className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        {turn.pending && !turn.content ? (
          <span className="inline-flex items-center gap-2 text-sm text-muted">
            <Spinner className="size-4" /> Reading your documents…
          </span>
        ) : (
          <p className={cn("whitespace-pre-wrap text-[15px] leading-relaxed", turn.error && "text-danger")}>{withCitations(turn.content, turn.citations)}</p>
        )}
        {turn.citations.length > 0 && (
          <div className="mt-3">
            <div className="mb-1.5 text-xs font-medium text-subtle">Sources</div>
            <ul className="grid gap-1.5 sm:grid-cols-2">
              {turn.citations.map((c) => (
                <li key={`${c.n}-${c.document_id}`}>
                  <Link to="/documents/$id" params={{ id: c.document_id }} search={{ page: c.page }} className="flex gap-2.5 rounded-lg border border-border bg-surface p-2.5 text-left hover:border-border-strong hover:bg-surface-2">
                    <span className="flex size-5 shrink-0 items-center justify-center rounded bg-accent-soft text-[11px] font-semibold text-accent-soft-fg">{c.n}</span>
                    <span className="min-w-0">
                      <span className="flex items-center gap-1 text-[13px] font-medium">
                        <FileText className="size-3.5 shrink-0 text-subtle" />
                        <span className="truncate">{c.title}</span>
                        <span className="shrink-0 font-normal text-subtle">p.{c.page}</span>
                      </span>
                      {c.snippet && <span className="mt-0.5 line-clamp-2 block text-xs text-muted">{c.snippet}</span>}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </div>
  );
}

/** Turns "[2]" markers in an answer into numbered chips that match the Sources list. */
function withCitations(text: string, cites: Citation[]): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(/\[(\d{1,2})\]/g)) {
    const c = cites.find((x) => x.n === Number(m[1]));
    out.push(text.slice(last, m.index));
    out.push(
      c ? (
        <Link key={m.index} to="/documents/$id" params={{ id: c.document_id }} search={{ page: c.page }} className="mx-0.5 inline-flex size-[18px] -translate-y-px items-center justify-center rounded bg-accent-soft align-middle text-[11px] font-semibold text-accent-soft-fg hover:bg-accent hover:text-accent-fg" title={`${c.title}, page ${c.page}`}>
          {c.n}
        </Link>
      ) : (
        m[0]
      ),
    );
    last = (m.index ?? 0) + m[0].length;
  }
  out.push(text.slice(last));
  return out;
}
