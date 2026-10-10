import * as React from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { differenceInCalendarDays, format, parseISO } from "date-fns";
import { ArrowDown, Check, Copy, FileText, History, MessageSquarePlus, MoreHorizontal, Pencil, RotateCcw, Search, SendHorizontal, Sparkles, Square, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { api, errorMessage } from "@/lib/api";
import { useAIEnabled, useDocument } from "@/lib/queries";
import type { Citation, Conversation } from "@/lib/types";
import { cn, spaceLabel, usePageTitle } from "@/lib/utils";
import { ask, claim, currentSession, forget, maxQuestion, newChat, openConversation, regenerate, setDocScope, stop, useChat, type Turn } from "@/stores/chat";
import { useCurrentUser } from "@/components/app-shell";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Input, NativeSelect, Textarea } from "@/components/ui/input";
import { EmptyState, LoadMore, Skeleton, Spinner } from "@/components/ui/misc";
import { Dialog, DialogContent, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/overlay";
import { confirm, prompt } from "@/components/ui/confirm";

const examples = ["When does my car insurance expire?", "How much was my last electricity bill?", "What did I pay for the laptop, and where is the warranty?"];
const docExamples = ["Summarise this document", "What are the important dates and amounts?", "Is anything due or expiring?"];
const pageSize = 40;

/** Pages of the conversation list (newest first), so long histories load in steps; q searches titles. */
function useConversationPages(q: string) {
  return useInfiniteQuery({
    queryKey: ["conversations", q],
    initialPageParam: "",
    queryFn: ({ pageParam }) => api.get<{ items: Conversation[] }>("/ai/conversations", { limit: pageSize, before: pageParam || undefined, q: q || undefined }).then((r) => r.items),
    getNextPageParam: (last) => (last.length === pageSize ? last[last.length - 1].updated_at : undefined),
  });
}

// The chat last open, so reopening Ask after a reload continues it (this browser only).
const lastKey = (user: string) => `docveta.ask.last.${user}`;
function readLast(user: string): string | null {
  try {
    return localStorage.getItem(lastKey(user));
  } catch {
    return null;
  }
}
function writeLast(user: string, id: string | null) {
  try {
    if (id) localStorage.setItem(lastKey(user), id);
    else localStorage.removeItem(lastKey(user));
  } catch {
    /* private mode */
  }
}

function useDebounced<T>(v: T, ms: number) {
  const [d, setD] = React.useState(v);
  React.useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}

/** Ask a question; the answer is written from your own documents and every claim cites its page. */
export function AskPage() {
  const ai = useAIEnabled();
  const me = useCurrentUser();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const search = useSearch({ from: "/app/ask" });
  usePageTitle("Ask your documents");
  React.useLayoutEffect(() => claim(me.id), [me.id]); // a different person signed in: start clean
  const session = useChat((st) => currentSession(st));
  const draft = useChat((st) => st.draft);
  const space = useChat((st) => st.space);
  const answering = useChat(useShallow((st) => Object.values(st.sessions).filter((s) => s.busy && s.convId).map((s) => s.convId as string)));
  const [filter, setFilter] = React.useState("");
  const q = useDebounced(filter.trim(), 250);
  const conversations = useConversationPages(q);
  const convList = React.useMemo(() => conversations.data?.pages.flat() ?? [], [conversations.data]);
  const [historyOpen, setHistoryOpen] = React.useState(false);
  const [atBottom, setAtBottom] = React.useState(true);
  const scroller = React.useRef<HTMLDivElement>(null);
  const input = React.useRef<HTMLTextAreaElement>(null);
  const { turns, busy, loading, convId, docId } = session;
  const scopeDoc = useDocument(docId ?? "", { enabled: !!docId });
  const listed = convList.find((c) => c.id === convId);

  // The address follows the open chat (?c=), so Back, reload and shared links come back to it;
  // opening /ask with ?c or ?doc picks the chat. Plain /ask shows the chat you left.
  React.useEffect(() => {
    const cur = currentSession();
    if (search.c) {
      if (cur.convId !== search.c)
        openConversation(search.c).catch((e) => {
          toast.error(errorMessage(e));
          forget(search.c!);
        });
    } else if (search.doc) {
      if (cur.docId !== search.doc) newChat(search.doc);
    } else if (!cur.convId && !cur.busy && cur.turns.length === 0) {
      // After a reload: back to the chat you were in.
      const last = readLast(me.id);
      if (last) openConversation(last).catch(() => writeLast(me.id, null));
    }
  }, [search.c, search.doc]);
  React.useEffect(() => {
    const cur = currentSession(); // may already be newer than this render (a chat was just opened)
    const want = { c: cur.convId ?? undefined, doc: cur.convId ? undefined : (cur.docId ?? undefined) };
    writeLast(me.id, cur.convId);
    if (want.c !== search.c || want.doc !== search.doc) void navigate({ to: "/ask", search: want, replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [convId, docId]);
  // Reopened from history: the conversation's own document scope.
  React.useEffect(() => {
    if (listed?.document_ids?.length && !docId) setDocScope(session.key, listed.document_ids[0]);
  }, [listed, docId, session.key]);

  // Follow the answer while it streams, unless the person scrolled up to read.
  const stick = React.useRef(true);
  React.useLayoutEffect(() => {
    stick.current = true;
    setAtBottom(true);
  }, [session.key]);
  React.useLayoutEffect(() => {
    if (!stick.current) return;
    const el = scroller.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [turns, session.key, loading]);
  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    stick.current = near;
    setAtBottom(near);
  };
  React.useEffect(() => {
    // Grow the question box with its text, up to a limit.
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, [draft]);
  React.useEffect(() => {
    if (!loading && window.matchMedia("(pointer: fine)").matches) input.current?.focus();
  }, [session.key, loading, busy]);

  const setDraft = (v: string) => useChat.setState({ draft: v });
  const send = (text: string) => {
    if (!text.trim() || busy || loading) return;
    if (text.trim().length > maxQuestion) return toast.error(`Questions can be up to ${maxQuestion} characters.`);
    setDraft("");
    stick.current = true;
    void ask(text);
  };
  const open = (c: Conversation) => {
    setHistoryOpen(false);
    if (c.id === convId) return;
    openConversation(c.id, c.document_ids?.[0] ?? null).catch((e) => {
      toast.error(errorMessage(e));
      forget(c.id);
      void qc.invalidateQueries({ queryKey: ["conversations"] });
    });
  };
  const startNew = (doc: string | null = null) => {
    setHistoryOpen(false);
    newChat(doc);
  };

  const remove = async (id: string) => {
    if (!(await confirm({ title: "Delete this conversation?", body: "Its questions and answers are removed. Your documents aren't touched.", confirmLabel: "Delete", destructive: true }))) return;
    try {
      await api.del(`/ai/conversations/${id}`);
    } catch (e) {
      return toast.error(errorMessage(e)); // still there: leave it open
    }
    forget(id);
    void qc.invalidateQueries({ queryKey: ["conversations"] });
  };
  const rename = async (c: Pick<Conversation, "id" | "title">) => {
    const title = await prompt({ title: "Rename conversation", confirmLabel: "Rename", defaultValue: c.title });
    if (!title?.trim() || title.trim() === c.title) return;
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
      return toast.error(errorMessage(e));
    }
    forget(null);
    newChat(null);
    void qc.invalidateQueries({ queryKey: ["conversations"] });
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
      filter={filter}
      onFilter={setFilter}
      searching={!!q}
      loading={conversations.isLoading}
      error={conversations.isError}
      active={convId}
      answering={answering}
      hasMore={!!conversations.hasNextPage}
      loadingMore={conversations.isFetchingNextPage}
      onMore={() => conversations.fetchNextPage()}
      onOpen={open}
      onRename={rename}
      onDelete={remove}
      onClearAll={clearAll}
    />
  );
  const title = convId ? (listed?.title ?? (turns.find((t) => t.role === "user")?.content || "Conversation")) : "New chat";
  const docTitle = scopeDoc.data?.title ?? (scopeDoc.isError ? "a document you can't open" : "this document");

  return (
    <div className="flex h-[calc(100dvh-3.5rem-5rem)] lg:h-[calc(100dvh-3.5rem)]">
      <aside className="hidden w-72 shrink-0 flex-col border-r border-border bg-sidebar/50 md:flex">
        <div className="p-3 pb-2">
          <Button className="w-full" onClick={() => startNew()}>
            <MessageSquarePlus /> New chat
          </Button>
        </div>
        {history}
      </aside>

      <section className="relative flex min-w-0 flex-1 flex-col">
        <header className="flex h-12 shrink-0 items-center gap-1.5 border-b border-border px-2 sm:px-3">
          <Button size="icon-sm" variant="ghost" className="md:hidden" onClick={() => setHistoryOpen(true)} aria-label="Conversations">
            <History />
          </Button>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-sm font-medium" title={title}>{title}</h1>
            {docId && (
              <p className="flex min-w-0 items-center gap-1 text-[11px] text-subtle">
                <FileText className="size-3 shrink-0" />
                <span className="truncate">About {docTitle}</span>
              </p>
            )}
          </div>
          {docId && scopeDoc.data && (
            <Button asChild size="sm" variant="ghost" className="hidden text-xs sm:inline-flex">
              <Link to="/documents/$id" params={{ id: docId }}>Open document</Link>
            </Button>
          )}
          {convId && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="icon-sm" variant="ghost" aria-label="Conversation options">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onSelect={() => rename({ id: convId, title: listed?.title ?? title })}>
                  <Pencil /> Rename…
                </DropdownMenuItem>
                <DropdownMenuItem destructive onSelect={() => remove(convId)}>
                  <Trash2 /> Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <Button size="icon-sm" variant="ghost" className="md:hidden" onClick={() => startNew()} aria-label="New chat">
            <MessageSquarePlus />
          </Button>
        </header>
        <Dialog open={historyOpen} onOpenChange={setHistoryOpen}>
          <DialogContent title="Conversations" size="sm" className="flex h-[80dvh] flex-col p-0">
            <div className="px-3 pt-1">
              <Button className="w-full" onClick={() => startNew()}>
                <MessageSquarePlus /> New chat
              </Button>
            </div>
            <div className="flex min-h-0 flex-1 flex-col pb-2">{history}</div>
          </DialogContent>
        </Dialog>

        <div ref={scroller} onScroll={onScroll} className="flex-1 overflow-y-auto scrollbar-thin" aria-live="polite" aria-busy={busy}>
          <div className="mx-auto max-w-3xl space-y-6 page-x py-6">
            {loading ? (
              <div className="space-y-6" aria-label="Loading the conversation">
                <Skeleton className="ml-auto h-10 w-2/3 rounded-2xl" />
                <Skeleton className="h-28" />
                <Skeleton className="ml-auto h-10 w-1/2 rounded-2xl" />
                <Skeleton className="h-20" />
              </div>
            ) : turns.length === 0 ? (
              <div className="pt-8 text-center sm:pt-16">
                <div className="mx-auto mb-4 flex size-12 items-center justify-center rounded-2xl bg-accent-soft text-accent">
                  <Sparkles className="size-6" />
                </div>
                <h2 className="text-2xl font-semibold tracking-tight">{docId ? "Ask about this document" : "Ask your documents"}</h2>
                <p className="mx-auto mt-2 max-w-md text-sm text-muted">
                  {docId ? `Answers come only from ${docTitle}, with the page they were found on.` : "Answers come from your own documents only, and show where they were found so you can check."}
                </p>
                <div className="mx-auto mt-6 grid max-w-xl gap-2 sm:grid-cols-3">
                  {(docId ? docExamples : examples).map((e) => (
                    <button key={e} onClick={() => send(e)} className="rounded-xl border border-border bg-surface px-4 py-3 text-left text-sm hover:border-border-strong hover:bg-surface-2">
                      {e}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              turns.map((t, i) => <TurnView key={t.id} turn={t} isLast={i === turns.length - 1} busy={busy} />)
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
            send(draft);
          }}
        >
          <div className="mx-auto flex max-w-3xl flex-col gap-2 page-x py-3">
            {docId && (
              <div className="flex min-w-0 items-center gap-2 self-start rounded-full border border-border bg-surface py-1 pl-2.5 pr-1 text-xs">
                <FileText className="size-3.5 shrink-0 text-subtle" />
                <span className="truncate">Asking about: {docTitle}</span>
                <button type="button" onClick={() => startNew(null)} className="rounded-full p-0.5 text-subtle hover:bg-surface-2 hover:text-fg" aria-label="New chat about all documents" title="New chat about all documents">
                  <X className="size-3.5" />
                </button>
              </div>
            )}
            <div className="flex items-end gap-2 rounded-xl border border-border bg-surface p-2 focus-within:border-accent focus-within:ring-3 focus-within:ring-ring">
              <Textarea
                ref={input}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    send(draft);
                  } else if (e.key === "Escape" && busy) {
                    stop();
                  }
                }}
                rows={1}
                maxLength={maxQuestion}
                disabled={loading}
                placeholder={docId ? "Ask about this document…" : turns.length ? "Ask a follow-up…" : "Ask anything about your documents…"}
                aria-label="Your question"
                className="max-h-[200px] min-h-9 flex-1 resize-none border-0 py-1.5 focus:ring-0"
              />
              {busy ? (
                <Button type="button" variant="secondary" size="icon" onClick={() => stop()} aria-label="Stop answering" title="Stop (Esc)">
                  <Square className="size-3.5 fill-current" />
                </Button>
              ) : (
                <Button type="submit" variant="primary" size="icon" disabled={!draft.trim() || loading} aria-label="Send">
                  <SendHorizontal />
                </Button>
              )}
            </div>
            <div className="flex min-h-5 flex-wrap items-center justify-between gap-2 text-xs text-subtle">
              {me.spaces.length > 1 && !docId ? (
                <label className="flex items-center gap-2">
                  Look in
                  <NativeSelect className="h-8 w-auto text-xs sm:h-8" value={space} onChange={(e) => useChat.setState({ space: e.target.value })}>
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
              <span className={cn("hidden sm:inline", draft.length > maxQuestion - 200 && "inline text-warning")}>
                {draft.length > maxQuestion - 200 ? `${draft.length} / ${maxQuestion}` : "Enter to send · Shift+Enter for a new line · AI can make mistakes; check the sources"}
              </span>
            </div>
          </div>
        </form>
      </section>
    </div>
  );
}

/** "Today", "Yesterday", "Previous 7 days", … like any chat app. */
function dayGroup(iso: string, now = new Date()): string {
  const d = parseISO(iso);
  const days = differenceInCalendarDays(now, d);
  if (days <= 0) return "Today";
  if (days === 1) return "Yesterday";
  if (days < 7) return "Previous 7 days";
  if (days < 30) return "Previous 30 days";
  return d.getFullYear() === now.getFullYear() ? format(d, "MMMM") : format(d, "MMMM yyyy");
}

function ConversationList({ items, filter, onFilter, searching, loading, error, active, answering, hasMore, loadingMore, onMore, onOpen, onRename, onDelete, onClearAll }: {
  items: Conversation[];
  filter: string;
  onFilter: (v: string) => void;
  searching: boolean;
  loading: boolean;
  error: boolean;
  active: string | null;
  answering: string[];
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
  onOpen: (c: Conversation) => void;
  onRename: (c: Conversation) => void;
  onDelete: (id: string) => void;
  onClearAll: () => void;
}) {
  let lastGroup = "";
  return (
    <>
      <div className="relative px-3 pb-2">
        <Search className="pointer-events-none absolute left-5.5 top-1/2 size-3.5 -translate-y-1/2 text-subtle" />
        <Input value={filter} onChange={(e) => onFilter(e.target.value)} placeholder="Search conversations" aria-label="Search conversations" className="h-8 pl-8 text-sm" />
      </div>
      <ul className="min-h-0 flex-1 overflow-y-auto scrollbar-thin px-2 pb-3">
        {loading && Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="m-2 h-8" />)}
        {!loading && error && <li className="px-3 py-6 text-center text-xs text-danger">Couldn't load your conversations.</li>}
        {!loading && !error && items.length === 0 && (
          <li className="px-3 py-6 text-center text-xs text-subtle">{searching ? "No conversation has that in its title." : "Your questions and answers will appear here."}</li>
        )}
        {items.map((c) => {
          const g = dayGroup(c.updated_at);
          const head = g !== lastGroup;
          lastGroup = g;
          const live = answering.includes(c.id);
          return (
            <React.Fragment key={c.id}>
              {head && <li className="px-2.5 pb-1 pt-3 text-[11px] font-medium uppercase tracking-wide text-subtle first:pt-1">{g}</li>}
              <li className="group relative">
                <button
                  onClick={() => onOpen(c)}
                  className={cn("flex w-full items-center gap-2 rounded-md px-2.5 py-2 pr-9 text-left hover:bg-surface-2", active === c.id && "bg-surface shadow-sm")}
                  title={c.title}
                  aria-current={active === c.id ? "true" : undefined}
                >
                  {c.document_ids?.length ? <FileText className="size-3.5 shrink-0 text-subtle" aria-label="About one document" /> : null}
                  <span className={cn("min-w-0 flex-1 truncate text-sm", active === c.id && "font-medium")}>{c.title || "Untitled"}</span>
                  {live && <Spinner className="size-3.5 shrink-0" />}
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
            </React.Fragment>
          );
        })}
        <li>
          <LoadMore hasMore={hasMore} loading={loadingMore} onMore={onMore} label="Show older" />
        </li>
      </ul>
      {items.length > 0 && !searching && (
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

const TurnView = React.memo(function TurnView({ turn, isLast, busy }: { turn: Turn; isLast: boolean; busy: boolean }) {
  const [copied, setCopied] = React.useState(false);
  const [allSources, setAllSources] = React.useState(false);
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Couldn't copy");
    }
  };
  if (turn.role === "user")
    return (
      <div className="group flex items-start justify-end gap-1">
        <button onClick={() => copy(turn.content)} className="mt-2 rounded p-1 text-subtle opacity-0 hover:bg-surface-2 hover:text-fg focus-visible:opacity-100 group-hover:opacity-100" aria-label="Copy question">
          {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
        </button>
        <div className="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-accent px-4 py-2.5 text-[15px] text-accent-fg">{turn.content}</div>
      </div>
    );
  // Sources the answer actually cites come first; the rest were searched but not used.
  const cited = new Set([...turn.content.matchAll(/\[(\d{1,2})\]/g)].map((m) => Number(m[1])));
  const used = turn.citations.filter((c) => cited.has(c.n));
  const other = turn.citations.filter((c) => !cited.has(c.n));
  const shown = turn.pending || used.length === 0 ? turn.citations : allSources ? [...used, ...other] : used;
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
              <Button size="sm" variant="ghost" className="h-7 px-2 text-xs text-subtle" onClick={() => copy(turn.content.replace(/\s?\[\d{1,2}\]/g, ""))}>
                {copied ? <Check /> : <Copy />} {copied ? "Copied" : "Copy"}
              </Button>
            )}
            {isLast && !busy && (
              <Button size="sm" variant="ghost" className="h-7 px-2 text-xs text-subtle" onClick={regenerate}>
                <RotateCcw /> {turn.error || turn.stopped ? "Try again" : "Regenerate"}
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
