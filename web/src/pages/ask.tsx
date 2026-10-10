import * as React from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, Check, Copy, FileText, History, MessageSquarePlus, MoreHorizontal, Pencil, RotateCcw, SendHorizontal, Sparkles, Square, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import { useAIEnabled, useDocument } from "@/lib/queries";
import type { Citation, Conversation, ConversationMessage } from "@/lib/types";
import { cn, spaceLabel, timeAgo, usePageTitle } from "@/lib/utils";
import { useCurrentUser } from "@/components/app-shell";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { NativeSelect, Textarea } from "@/components/ui/input";
import { EmptyState, Skeleton, Spinner } from "@/components/ui/misc";
import { Dialog, DialogContent, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/overlay";
import { confirm, prompt } from "@/components/ui/confirm";

interface Turn {
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  pending?: boolean;
  stage?: "searching" | "answering";
  error?: boolean;
  stopped?: boolean;
}

const examples = ["When does my car insurance expire?", "How much was my last electricity bill?", "What did I pay for the laptop, and where is the warranty?"];
const maxQuestion = 2000;

/** Pages of the conversation list (newest first), so long histories load in steps. */
function useConversationPages() {
  return useInfiniteQuery({
    queryKey: ["conversations"],
    initialPageParam: "",
    queryFn: ({ pageParam }) => api.get<{ items: Conversation[] }>("/ai/conversations", { limit: 40, before: pageParam || undefined }).then((r) => r.items),
    getNextPageParam: (last) => (last.length === 40 ? last[last.length - 1].updated_at : undefined),
  });
}

/** Ask a question; the answer is written from your own documents and every claim cites its page. */
export function AskPage() {
  const ai = useAIEnabled();
  const me = useCurrentUser();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const search = useSearch({ from: "/app/ask" });
  usePageTitle("Ask your documents");
  const conversations = useConversationPages();
  const convList = React.useMemo(() => conversations.data?.pages.flat() ?? [], [conversations.data]);
  const [convId, setConvId] = React.useState<string | null>(null);
  const [turns, setTurns] = React.useState<Turn[]>([]);
  const [text, setText] = React.useState("");
  const [space, setSpace] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [loading, setLoading] = React.useState(false);
  const [historyOpen, setHistoryOpen] = React.useState(false);
  const [atBottom, setAtBottom] = React.useState(true);
  const scroller = React.useRef<HTMLDivElement>(null);
  const input = React.useRef<HTMLTextAreaElement>(null);
  const abort = React.useRef<AbortController | null>(null);
  const docId = search.doc;
  const scopeDoc = useDocument(docId ?? "", { enabled: !!docId });

  // Follow the answer while it streams, unless the person scrolled up to read.
  const stick = React.useRef(true);
  React.useEffect(() => {
    if (!stick.current) return;
    const el = scroller.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [turns]);
  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    stick.current = near;
    setAtBottom(near);
  };
  React.useEffect(() => () => abort.current?.abort(), []);
  React.useEffect(() => {
    // Grow the question box with its text, up to a limit.
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, [text]);

  const open = async (id: string | null) => {
    abort.current?.abort();
    setHistoryOpen(false);
    setConvId(id);
    setTurns([]);
    stick.current = true;
    if (!id) {
      input.current?.focus();
      return;
    }
    setLoading(true);
    try {
      const r = await api.get<{ items: ConversationMessage[] }>(`/ai/conversations/${id}/messages`);
      setTurns(r.items.map((m) => ({ role: m.role, content: m.content, citations: m.citations ?? [] })));
    } catch (e) {
      toast.error(errorMessage(e));
      setConvId(null);
    } finally {
      setLoading(false);
    }
  };

  const stop = () => abort.current?.abort();

  const ask = async (question: string) => {
    const q = question.trim();
    if (!q || busy) return;
    if (q.length > maxQuestion) {
      toast.error(`Questions can be up to ${maxQuestion} characters.`);
      return;
    }
    setText("");
    setBusy(true);
    stick.current = true;
    setTurns((t) => [...t, { role: "user", content: q, citations: [] }, { role: "assistant", content: "", citations: [], pending: true, stage: "searching" }]);
    const patchLast = (f: (t: Turn) => Turn) => setTurns((t) => t.map((x, i) => (i === t.length - 1 ? f(x) : x)));
    // The answer arrives a few letters at a time. Showing each piece on its own lays the whole
    // answer out again every time; collect what came in during a frame and show it at once.
    let waiting = "";
    let raf = 0;
    const flush = () => {
      cancelAnimationFrame(raf);
      raf = 0;
      if (!waiting) return;
      const add = waiting;
      waiting = "";
      patchLast((t) => ({ ...t, content: t.content + add }));
    };
    const ctl = new AbortController();
    abort.current = ctl;
    try {
      const res = await fetch("/api/v1/ai/ask", {
        method: "POST",
        credentials: "same-origin",
        signal: ctl.signal,
        headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
        body: JSON.stringify({ question: q, conversation_id: convId ?? undefined, space_ids: space ? [space] : undefined, document_ids: docId ? [docId] : undefined }),
      });
      if (!res.ok || !res.body) {
        const b = (await res.json().catch(() => ({}))) as { title?: string };
        throw new Error(b.title || (res.status >= 500 ? "The server couldn't answer right now. Try again in a moment." : "Couldn't get an answer"));
      }
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      let finished = false;
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
          if (!ev || data === undefined) continue; // keep-alive comments
          let v: unknown;
          try {
            v = JSON.parse(data);
          } catch {
            continue; // one unreadable message shouldn't throw the answer away
          }
          if (ev === "delta") {
            waiting += v as string;
            if (!raf) raf = requestAnimationFrame(flush);
            continue;
          }
          flush();
          if (ev === "status") patchLast((t) => ({ ...t, stage: (v as { stage: Turn["stage"] }).stage }));
          else if (ev === "citations") patchLast((t) => ({ ...t, citations: v as Citation[] }));
          else if (ev === "error") patchLast((t) => ({ ...t, content: t.content ? `${t.content}\n\n${(v as { message: string }).message}` : (v as { message: string }).message, error: true }));
          else if (ev === "done") {
            finished = true;
            setConvId((v as { conversation_id: string }).conversation_id);
          }
        }
      }
      flush();
      if (!finished) patchLast((t) => (t.error ? t : { ...t, error: !t.content, content: t.content || "The connection closed before the answer finished. Try again." }));
    } catch (e) {
      flush(); // keep what had arrived
      if ((e as Error).name === "AbortError") patchLast((t) => ({ ...t, stopped: true }));
      else patchLast((t) => ({ ...t, content: t.content ? `${t.content}\n\n${errorMessage(e)}` : errorMessage(e), error: true }));
    } finally {
      patchLast((t) => ({ ...t, pending: false }));
      setBusy(false);
      abort.current = null;
      qc.invalidateQueries({ queryKey: ["conversations"] });
    }
  };

  const retry = () => {
    const lastQ = [...turns].reverse().find((t) => t.role === "user")?.content;
    if (!lastQ) return;
    setTurns((t) => t.slice(0, -2));
    void ask(lastQ);
  };

  // The same function every time, so finished turns aren't drawn again while a new answer streams in.
  const retryNow = React.useRef(retry);
  retryNow.current = retry;
  const onRetry = React.useCallback(() => retryNow.current(), []);

  const remove = async (id: string) => {
    if (!(await confirm({ title: "Delete this conversation?", confirmLabel: "Delete", destructive: true }))) return;
    try {
      await api.del(`/ai/conversations/${id}`);
    } catch (e) {
      toast.error(errorMessage(e)); // still there: leave it open
      return;
    }
    qc.invalidateQueries({ queryKey: ["conversations"] });
    if (convId === id) void open(null);
  };
  const rename = async (c: Conversation) => {
    const title = await prompt({ title: "Rename conversation", confirmLabel: "Rename", defaultValue: c.title });
    if (!title?.trim()) return;
    await api.patch(`/ai/conversations/${c.id}`, { title }).then(
      () => qc.invalidateQueries({ queryKey: ["conversations"] }),
      (e) => toast.error(errorMessage(e)),
    );
  };
  const clearAll = async () => {
    if (!(await confirm({ title: "Delete all conversations?", body: "Your questions and answers are removed. Your documents aren't touched.", confirmLabel: "Delete all", destructive: true }))) return;
    try {
      await api.del("/ai/conversations");
    } catch (e) {
      toast.error(errorMessage(e));
      return;
    }
    qc.invalidateQueries({ queryKey: ["conversations"] });
    void open(null);
  };

  if (ai.isLoading) return <div className="flex h-full items-center justify-center"><Spinner /></div>;
  if (!ai.data?.chat)
    return (
      <EmptyState icon={<Sparkles />} title="Ask needs an AI provider" className="min-h-[60vh]" action={me.is_admin ? <Button asChild variant="primary"><Link to="/admin/$section" params={{ section: "ai" }}>Set up AI</Link></Button> : undefined}>
        {me.is_admin ? "Connect any OpenAI-compatible service, or one running on your own computer (Ollama, LM Studio, llama.cpp)." : "Ask your administrator to connect an AI provider."}
      </EmptyState>
    );

  const history = (
    <ConversationList
      items={convList}
      loading={conversations.isLoading}
      active={convId}
      hasMore={!!conversations.hasNextPage}
      loadingMore={conversations.isFetchingNextPage}
      onMore={() => conversations.fetchNextPage()}
      onOpen={open}
      onRename={rename}
      onDelete={remove}
      onClearAll={clearAll}
    />
  );
  const lastTurn = turns[turns.length - 1];

  return (
    <div className="flex h-[calc(100dvh-3.5rem-5rem)] lg:h-[calc(100dvh-3.5rem)]">
      <aside className="hidden w-72 shrink-0 flex-col border-r border-border bg-sidebar/50 md:flex">
        <div className="p-3">
          <Button className="w-full" onClick={() => open(null)}>
            <MessageSquarePlus /> New conversation
          </Button>
        </div>
        {history}
      </aside>

      <section className="relative flex min-w-0 flex-1 flex-col">
        {/* Phones: the conversation list lives in a sheet. */}
        <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-3 md:hidden">
          <Button size="sm" variant="ghost" onClick={() => setHistoryOpen(true)}>
            <History /> History
          </Button>
          <span className="min-w-0 flex-1 truncate text-sm font-medium">{convList.find((c) => c.id === convId)?.title ?? ""}</span>
          <Button size="icon-sm" variant="ghost" onClick={() => open(null)} aria-label="New conversation">
            <MessageSquarePlus />
          </Button>
        </div>
        <Dialog open={historyOpen} onOpenChange={setHistoryOpen}>
          <DialogContent title="Conversations" size="sm" className="flex max-h-[80dvh] flex-col p-0">
            <div className="flex min-h-0 flex-1 flex-col pb-2">{history}</div>
          </DialogContent>
        </Dialog>

        <div ref={scroller} onScroll={onScroll} className="flex-1 overflow-y-auto scrollbar-thin">
          <div className="mx-auto max-w-3xl space-y-6 page-x py-6">
            {loading ? (
              <div className="space-y-4">
                <Skeleton className="ml-auto h-10 w-2/3" />
                <Skeleton className="h-32" />
              </div>
            ) : turns.length === 0 ? (
              <div className="pt-8 text-center sm:pt-16">
                <div className="mx-auto mb-4 flex size-12 items-center justify-center rounded-2xl bg-accent-soft text-accent">
                  <Sparkles className="size-6" />
                </div>
                <h1 className="text-2xl font-semibold tracking-tight">{docId ? "Ask about this document" : "Ask your documents"}</h1>
                <p className="mx-auto mt-2 max-w-md text-sm text-muted">
                  Answers come from your own documents only, and show where they were found so you can check.
                </p>
                <div className="mx-auto mt-6 flex max-w-lg flex-col gap-2">
                  {(docId ? ["Summarise this document", "What are the important dates and amounts?", "Is anything due or expiring?"] : examples).map((e) => (
                    <button key={e} onClick={() => ask(e)} className="rounded-lg border border-border bg-surface px-4 py-2.5 text-left text-sm hover:border-border-strong hover:bg-surface-2">
                      {e}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              turns.map((t, i) => <TurnView key={i} turn={t} isLast={i === turns.length - 1} onRetry={onRetry} />)
            )}
          </div>
        </div>

        {!atBottom && turns.length > 0 && (
          <button
            onClick={() => {
              stick.current = true;
              scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: "smooth" });
            }}
            className="absolute bottom-36 left-1/2 z-10 flex size-9 -translate-x-1/2 items-center justify-center rounded-full border border-border bg-surface shadow-md hover:bg-surface-2"
            aria-label="Scroll to the newest message"
          >
            <ArrowDown className="size-4" />
          </button>
        )}

        <form
          className="border-t border-border bg-bg/90 backdrop-blur"
          onSubmit={(e) => {
            e.preventDefault();
            void ask(text);
          }}
        >
          <div className="mx-auto flex max-w-3xl flex-col gap-2 page-x py-3">
            {docId && (
              <div className="flex min-w-0 items-center gap-2 self-start rounded-full border border-border bg-surface py-1 pl-2.5 pr-1 text-xs">
                <FileText className="size-3.5 shrink-0 text-subtle" />
                <span className="truncate">Asking about: {scopeDoc.data?.title ?? "this document"}</span>
                <button type="button" onClick={() => navigate({ to: "/ask", search: {} })} className="rounded-full p-0.5 text-subtle hover:bg-surface-2 hover:text-fg" aria-label="Ask about all documents instead">
                  <X className="size-3.5" />
                </button>
              </div>
            )}
            <div className="flex items-end gap-2 rounded-xl border border-border bg-surface p-2 focus-within:border-accent focus-within:ring-3 focus-within:ring-ring">
              <Textarea
                ref={input}
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void ask(text);
                  } else if (e.key === "Escape" && busy) {
                    stop();
                  }
                }}
                rows={1}
                maxLength={maxQuestion}
                placeholder={docId ? "Ask about this document…" : "Ask anything about your documents…"}
                aria-label="Your question"
                className="max-h-[200px] min-h-9 flex-1 resize-none border-0 py-1.5 focus:ring-0"
              />
              {busy ? (
                <Button type="button" variant="secondary" size="icon" onClick={stop} aria-label="Stop answering" title="Stop (Esc)">
                  <Square className="size-3.5 fill-current" />
                </Button>
              ) : (
                <Button type="submit" variant="primary" size="icon" disabled={!text.trim()} aria-label="Ask">
                  <SendHorizontal />
                </Button>
              )}
            </div>
            <div className="flex min-h-5 flex-wrap items-center justify-between gap-2 text-xs text-subtle">
              {me.spaces.length > 1 && !docId ? (
                <label className="flex items-center gap-2">
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
              ) : (
                <span />
              )}
              <span className="hidden sm:inline">
                {text.length > maxQuestion - 200 ? `${text.length} / ${maxQuestion}` : "Enter to send · Shift+Enter for a new line"}
              </span>
            </div>
            {lastTurn?.error && !busy && <span className="sr-only" role="alert">{lastTurn.content}</span>}
          </div>
        </form>
      </section>
    </div>
  );
}

function ConversationList({ items, loading, active, hasMore, loadingMore, onMore, onOpen, onRename, onDelete, onClearAll }: {
  items: Conversation[];
  loading: boolean;
  active: string | null;
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
  onOpen: (id: string) => void;
  onRename: (c: Conversation) => void;
  onDelete: (id: string) => void;
  onClearAll: () => void;
}) {
  return (
    <>
      <ul className="min-h-0 flex-1 space-y-0.5 overflow-y-auto scrollbar-thin px-2 pb-3">
        {loading && Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="m-2 h-8" />)}
        {!loading && items.length === 0 && <li className="px-3 py-6 text-center text-xs text-subtle">Your questions and answers will appear here.</li>}
        {items.map((c) => (
          <li key={c.id} className="group relative">
            <button
              onClick={() => onOpen(c.id)}
              className={cn("block w-full rounded-md px-2.5 py-2 pr-9 text-left hover:bg-surface-2", active === c.id && "bg-surface shadow-sm")}
              title={c.title}
            >
              <span className={cn("block truncate text-sm", active === c.id && "font-medium")}>{c.title || "Untitled"}</span>
              <span className="block text-[11px] text-subtle">{timeAgo(c.updated_at)}</span>
            </button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button aria-label={`Options for ${c.title || "conversation"}`} className="absolute right-1 top-1/2 -translate-y-1/2 rounded p-1.5 text-subtle opacity-100 hover:bg-surface-3 hover:text-fg focus-visible:opacity-100 md:opacity-0 md:group-hover:opacity-100 data-[state=open]:opacity-100">
                  <MoreHorizontal className="size-4" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onSelect={() => onRename(c)}>
                  <Pencil /> Rename…
                </DropdownMenuItem>
                <DropdownMenuItem destructive onSelect={() => onDelete(c.id)}>
                  <Trash2 /> Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </li>
        ))}
        {hasMore && (
          <li className="p-2 text-center">
            <Button size="sm" variant="ghost" loading={loadingMore} onClick={onMore}>
              Show older
            </Button>
          </li>
        )}
      </ul>
      {items.length > 0 && (
        <div className="border-t border-border p-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm" variant="ghost" className="w-full justify-start text-subtle">
                <MoreHorizontal /> Manage history
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem destructive onSelect={onClearAll}>
                <Trash2 /> Delete all conversations…
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem disabled>Kept until you delete them</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      )}
    </>
  );
}

const TurnView = React.memo(function TurnView({ turn, isLast, onRetry }: { turn: Turn; isLast: boolean; onRetry: () => void }) {
  const [copied, setCopied] = React.useState(false);
  const [allSources, setAllSources] = React.useState(false);
  if (turn.role === "user")
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-accent px-4 py-2.5 text-[15px] text-accent-fg">{turn.content}</div>
      </div>
    );
  // Sources the answer actually cites come first; the rest were searched but not used.
  const cited = new Set([...turn.content.matchAll(/\[(\d{1,2})\]/g)].map((m) => Number(m[1])));
  const used = turn.citations.filter((c) => cited.has(c.n));
  const other = turn.citations.filter((c) => !cited.has(c.n));
  const shown = turn.pending || used.length === 0 ? turn.citations : allSources ? [...used, ...other] : used;
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(turn.content.replace(/\s?\[\d{1,2}\]/g, ""));
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Couldn't copy");
    }
  };
  return (
    <div className="flex gap-3">
      <span className="mt-1 flex size-7 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
        <Sparkles className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        {turn.pending && !turn.content ? (
          <span className="inline-flex items-center gap-2 text-sm text-muted" role="status">
            <Spinner className="size-4" /> {turn.stage === "answering" ? "Writing the answer…" : "Searching your documents…"}
          </span>
        ) : turn.error && !turn.content.includes("\n\n") ? (
          <div className="rounded-lg border border-danger/30 bg-danger-soft px-3 py-2.5 text-sm text-danger" role="alert">{turn.content}</div>
        ) : (
          <Markdown text={turn.content} inline={(s, key) => withCitations(s, turn.citations, key)} className={cn(turn.error && "text-danger")} />
        )}
        {turn.pending && turn.content && <span className="ml-0.5 inline-block h-4 w-1.5 animate-pulse rounded-sm bg-accent align-middle" aria-hidden />}
        {turn.stopped && <p className="mt-2 text-xs text-subtle">Stopped.</p>}
        {!turn.pending && (
          <div className="mt-2 flex items-center gap-1">
            {turn.content && !turn.error && (
              <Button size="sm" variant="ghost" className="h-7 px-2 text-xs text-subtle" onClick={copy}>
                {copied ? <Check /> : <Copy />} {copied ? "Copied" : "Copy"}
              </Button>
            )}
            {isLast && (turn.error || turn.stopped) && (
              <Button size="sm" variant="ghost" className="h-7 px-2 text-xs text-subtle" onClick={onRetry}>
                <RotateCcw /> Try again
              </Button>
            )}
          </div>
        )}
        {shown.length > 0 && (
          <div className="mt-3">
            <div className="mb-1.5 text-xs font-medium text-subtle">{turn.pending ? "Looking at" : "Sources"}</div>
            <ul className="grid gap-1.5 sm:grid-cols-2">
              {shown.map((c) => (
                <li key={`${c.n}-${c.document_id}-${c.page}`} className="min-w-0">
                  <Link to="/documents/$id" params={{ id: c.document_id }} search={{ page: c.page }} className="flex gap-2.5 rounded-lg border border-border bg-surface p-2.5 text-left hover:border-border-strong hover:bg-surface-2">
                    <span className="flex size-5 shrink-0 items-center justify-center rounded bg-accent-soft text-[11px] font-semibold text-accent-soft-fg">{c.n}</span>
                    <span className="min-w-0 flex-1">
                      <span className="flex min-w-0 items-center gap-1 text-[13px] font-medium">
                        <FileText className="size-3.5 shrink-0 text-subtle" />
                        <span className="truncate" title={c.title}>{c.title}</span>
                        <span className="shrink-0 font-normal text-subtle">p.{c.page}</span>
                      </span>
                      {c.snippet && <span className="mt-0.5 line-clamp-2 block break-words text-xs text-muted">{c.snippet}</span>}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
            {!turn.pending && used.length > 0 && other.length > 0 && (
              <button onClick={() => setAllSources((v) => !v)} className="mt-1.5 text-xs text-subtle hover:text-fg">
                {allSources ? "Show only cited sources" : `Also searched ${other.length} more passage${other.length === 1 ? "" : "s"}`}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
});

/** Turns "[2]" markers in an answer into numbered chips that link to the cited page. */
function withCitations(text: string, cites: Citation[], key: string): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(/\[(\d{1,2})\]/g)) {
    const c = cites.find((x) => x.n === Number(m[1]));
    out.push(text.slice(last, m.index));
    out.push(
      c ? (
        <Link key={`${key}.${m.index}`} to="/documents/$id" params={{ id: c.document_id }} search={{ page: c.page }} className="mx-0.5 inline-flex size-[18px] -translate-y-px items-center justify-center rounded bg-accent-soft align-middle text-[11px] font-semibold text-accent-soft-fg no-underline hover:bg-accent hover:text-accent-fg" title={`${c.title}, page ${c.page}`}>
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
