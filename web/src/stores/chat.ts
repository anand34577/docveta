import { create } from "zustand";
import { api, errorMessage } from "@/lib/api";
import { queryClient } from "@/lib/queries";
import type { Citation, ConversationMessage } from "@/lib/types";

export interface Turn {
  id: number;
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  pending?: boolean;
  stage?: "searching" | "answering";
  error?: boolean;
  stopped?: boolean;
  /** The server kept this answer (so asking again replaces it rather than adding to it). */
  saved?: boolean;
}

/** One open chat. It lives here, not in the page, so leaving the page keeps it and its answer keeps arriving. */
export interface Session {
  key: string;
  convId: string | null;
  docId: string | null;
  turns: Turn[];
  busy: boolean;
  loading: boolean;
}

interface ChatState {
  owner: string | null;
  sessions: Record<string, Session>;
  current: string;
  draft: string;
  space: string;
}

export const maxQuestion = 2000;

let seq = 0;
const nextId = () => ++seq;
const blank = (docId: string | null = null): Session => ({ key: `s${nextId()}`, convId: null, docId, turns: [], busy: false, loading: false });
const first = blank();

export const useChat = create<ChatState>(() => ({ owner: null, sessions: { [first.key]: first }, current: first.key, draft: "", space: "" }));

const aborts = new Map<string, AbortController>();
const get = () => useChat.getState();
const set = useChat.setState;

function patch(key: string, f: (s: Session) => Partial<Session>) {
  set((st) => {
    const s = st.sessions[key];
    if (!s) return st; // closed meanwhile (deleted, signed out)
    const next = { ...s, ...f(s) };
    // A finished chat nobody is looking at is on the server now; drop the copy.
    if (key !== st.current && !next.busy && !next.loading) {
      const { [key]: _, ...rest } = st.sessions;
      return { sessions: rest };
    }
    return { sessions: { ...st.sessions, [key]: next } };
  });
}
const patchLast = (key: string, f: (t: Turn) => Partial<Turn>) => patch(key, (s) => ({ turns: s.turns.map((t, i) => (i === s.turns.length - 1 ? { ...t, ...f(t) } : t)) }));

/** Shows session s, dropping the one shown before unless it is still answering. */
function show(s: Session) {
  set((st) => {
    const sessions = { ...st.sessions, [s.key]: s };
    const prev = st.sessions[st.current];
    if (prev && prev.key !== s.key && !prev.busy) delete sessions[prev.key];
    return { sessions, current: s.key };
  });
}

const currentTurnContent = (key: string) => get().sessions[key]?.turns.at(-1)?.content;

export const currentSession = (st: ChatState = get()) => st.sessions[st.current];

/** Forgets everything when a different person signs in on this browser. */
export function claim(userId: string) {
  if (get().owner === userId) return;
  aborts.forEach((a) => a.abort());
  aborts.clear();
  const s = blank();
  set({ owner: userId, sessions: { [s.key]: s }, current: s.key, draft: "", space: "" });
}

export function newChat(docId: string | null = null) {
  const cur = currentSession();
  if (cur && !cur.busy && !cur.loading && cur.turns.length === 0 && !cur.convId) {
    if (cur.docId !== docId) patch(cur.key, () => ({ docId }));
    return;
  }
  show(blank(docId));
}

/** Opens a saved conversation; one still answering is shown as it is. */
export async function openConversation(id: string, docId: string | null = null) {
  const live = Object.values(get().sessions).find((s) => s.convId === id);
  if (live) {
    if (live.key !== get().current) show(live);
    return;
  }
  const s: Session = { ...blank(docId), convId: id, loading: true };
  show(s);
  try {
    const r = await api.get<{ items: ConversationMessage[] }>(`/ai/conversations/${id}/messages`);
    patch(s.key, () => ({ loading: false, turns: r.items.map((m) => ({ id: nextId(), role: m.role, content: m.content, citations: m.citations ?? [], saved: true })) }));
  } catch (e) {
    patch(s.key, () => ({ loading: false }));
    throw e;
  }
}

export function setDocScope(key: string, docId: string) {
  patch(key, (s) => (s.docId ? {} : { docId }));
}

export function stop(key = get().current) {
  aborts.get(key)?.abort();
}

/** Closes conversation id wherever it is open (it was deleted). */
export function forget(id: string | null) {
  for (const s of Object.values(get().sessions)) {
    if (id !== null && s.convId !== id) continue;
    aborts.get(s.key)?.abort();
    aborts.delete(s.key);
    set((st) => {
      const { [s.key]: _, ...rest } = st.sessions;
      return { sessions: rest };
    });
  }
  if (!currentSession()) show(blank());
}

/** Asks again: the last answer is replaced, here and in the saved conversation. */
export function regenerate() {
  const s = currentSession();
  const n = s.turns.length;
  if (s.busy || n < 2 || s.turns[n - 1].role !== "assistant") return;
  const q = s.turns[n - 2].content;
  const replace = !!s.convId && !!s.turns[n - 1].saved;
  patch(s.key, (x) => ({ turns: x.turns.slice(0, -2) }));
  void ask(q, replace);
}

export async function ask(question: string, replace = false) {
  const q = question.trim();
  const s = currentSession();
  if (!q || !s || s.busy || s.loading) return;
  if (q.length > maxQuestion) throw new Error(`Questions can be up to ${maxQuestion} characters.`);
  const key = s.key;
  const space = get().space;
  patch(key, (x) => ({
    busy: true,
    turns: [...x.turns, { id: nextId(), role: "user", content: q, citations: [] }, { id: nextId(), role: "assistant", content: "", citations: [], pending: true, stage: "searching" }],
  }));
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
    patchLast(key, (t) => ({ content: t.content + add }));
  };
  // A new conversation's id, known from the start: an answer stopped part-way is saved under it.
  let startedConv = "";
  const ctl = new AbortController();
  aborts.set(key, ctl);
  try {
    const res = await fetch("/api/v1/ai/ask", {
      method: "POST",
      credentials: "same-origin",
      signal: ctl.signal,
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify({
        question: q,
        conversation_id: s.convId ?? undefined,
        space_ids: space && !s.docId ? [space] : undefined,
        document_ids: s.docId && !s.convId ? [s.docId] : undefined,
        regenerate: replace || undefined,
      }),
    });
    if (!res.ok || !res.body) {
      const b = (await res.json().catch(() => ({}))) as { title?: string; detail?: string };
      if (res.status === 404 && s.convId) {
        // Deleted elsewhere (another tab, the phone): carry on in a new conversation.
        patch(key, () => ({ convId: null }));
        throw new Error("This conversation was deleted. Ask again to start a new one.");
      }
      throw new Error(b.detail || b.title || (res.status >= 500 ? "The server couldn't answer right now. Try again in a moment." : "Couldn't get an answer"));
    }
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    let finished = false;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true }).replace(/\r\n/g, "\n");
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
        if (ev === "status") {
          const st = v as { stage: Turn["stage"]; conversation_id?: string };
          if (st.conversation_id) startedConv = st.conversation_id;
          patchLast(key, () => ({ stage: st.stage }));
        }
        else if (ev === "citations") patchLast(key, () => ({ citations: v as Citation[] }));
        else if (ev === "error") patchLast(key, (t) => ({ content: t.content ? `${t.content}\n\n${(v as { message: string }).message}` : (v as { message: string }).message, error: true }));
        else if (ev === "done") {
          finished = true;
          patch(key, () => ({ convId: (v as { conversation_id: string }).conversation_id }));
          patchLast(key, () => ({ saved: true }));
        }
      }
    }
    flush();
    if (!finished) patchLast(key, (t) => (t.error ? {} : { error: !t.content, content: t.content || "The connection closed before the answer finished. Try again." }));
  } catch (e) {
    flush(); // keep what had arrived
    if ((e as Error).name === "AbortError") {
      const kept = !!currentTurnContent(key);
      patchLast(key, () => ({ stopped: true, saved: kept }));
      if (kept && startedConv) patch(key, (x) => (x.convId ? {} : { convId: startedConv }));
    }
    else patchLast(key, (t) => ({ content: t.content ? `${t.content}\n\n${errorMessage(e)}` : errorMessage(e), error: true }));
  } finally {
    if (aborts.get(key) === ctl) aborts.delete(key);
    patchLast(key, () => ({ pending: false }));
    patch(key, () => ({ busy: false }));
    void queryClient.invalidateQueries({ queryKey: ["conversations"] });
  }
}
