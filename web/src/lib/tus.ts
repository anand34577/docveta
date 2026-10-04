// Resumable uploads (tus 1.0.0) for big files and flaky connections: the upload carries on from
// where it stopped after a network drop, a closed tab or a reload.
import { ApiError, type FieldError, type UploadOptions } from "./api";

const TUS = { "Tus-Resumable": "1.0.0" };
const CHUNK = 4 * 1024 * 1024;
const RETRIES = 6;

/** Files above this size go through tus. */
export const TUS_THRESHOLD = 8 * 1024 * 1024;

const storeKey = (o: UploadOptions) => `docveta.tus:${o.file.name}:${o.file.size}:${o.file.lastModified}:${o.spaceId ?? ""}`;
const remember = (k: string, v: string | null) => {
  try {
    if (v) localStorage.setItem(k, v);
    else localStorage.removeItem(k);
  } catch {
    /* private mode: resuming after a reload just won't work */
  }
};
const recall = (k: string) => {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
};

const b64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)));
const sleep = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((res, rej) => {
    const t = setTimeout(res, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(t);
      rej(new DOMException("Aborted", "AbortError"));
    });
  });

async function problem(res: Response): Promise<ApiError> {
  let body: { title?: string; code?: string; errors?: FieldError[]; extra?: Record<string, unknown> } = {};
  try {
    body = await res.json();
  } catch {
    /* no body */
  }
  return new ApiError(res.status, body.code || "error", body.title || (res.status === 413 ? "This file is too large" : `Upload failed (${res.status})`), body.errors || [], body.extra || {});
}

/** Sends one chunk with progress. Resolves with the response status and headers. */
function patchChunk(url: string, offset: number, blob: Blob, onBytes: (n: number) => void, signal?: AbortSignal) {
  return new Promise<{ status: number; headers: (n: string) => string | null; body: string }>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PATCH", url);
    xhr.withCredentials = true;
    xhr.setRequestHeader("Tus-Resumable", TUS["Tus-Resumable"]);
    xhr.setRequestHeader("Content-Type", "application/offset+octet-stream");
    xhr.setRequestHeader("Upload-Offset", String(offset));
    xhr.upload.onprogress = (e) => e.lengthComputable && onBytes(e.loaded);
    xhr.onload = () => resolve({ status: xhr.status, headers: (n) => xhr.getResponseHeader(n), body: xhr.responseText });
    xhr.onerror = () => reject(new ApiError(0, "network", "Connection lost"));
    xhr.onabort = () => reject(new DOMException("Aborted", "AbortError"));
    signal?.addEventListener("abort", () => xhr.abort());
    xhr.send(blob);
  });
}

async function headOffset(url: string, signal?: AbortSignal): Promise<number | null> {
  try {
    const r = await fetch(url, { method: "HEAD", headers: TUS, credentials: "same-origin", signal });
    if (r.status === 404 || r.status === 410) return null;
    if (!r.ok) throw new ApiError(r.status, "error", `Upload failed (${r.status})`);
    return Number(r.headers.get("Upload-Offset") ?? 0);
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    if (e instanceof ApiError) throw e;
    throw new ApiError(0, "network", "Connection lost");
  }
}

export async function tusUpload<T>(o: UploadOptions, fetchDocument: (id: string) => Promise<T>): Promise<T> {
  const key = storeKey(o);
  let url = recall(key);
  let offset = 0;
  if (url) {
    const off = await headOffset(url, o.signal).catch((e) => (e instanceof ApiError && e.status === 0 ? Promise.reject(e) : null));
    if (off === null) {
      remember(key, null);
      url = null;
    } else offset = off;
  }
  if (!url) {
    const meta: Record<string, string> = { filename: o.file.name, filetype: o.file.type };
    if (o.spaceId) meta.space_id = o.spaceId;
    if (o.tagIds?.length) meta.tag_ids = o.tagIds.join(",");
    if (o.allowDuplicate) meta.allow_duplicate = "true";
    if (o.source) meta.source = o.source;
    const res = await fetch("/api/v1/uploads", {
      method: "POST",
      credentials: "same-origin",
      signal: o.signal,
      headers: { ...TUS, "Upload-Length": String(o.file.size), "Upload-Metadata": Object.entries(meta).map(([k, v]) => `${k} ${b64(v)}`).join(",") },
    }).catch((e) => {
      if ((e as Error).name === "AbortError") throw e;
      throw new ApiError(0, "network", "Upload failed: can't reach the server");
    });
    if (!res.ok) throw await problem(res);
    url = res.headers.get("Location");
    if (!url) throw new ApiError(0, "error", "The server didn't start the upload");
    remember(key, url);
    offset = 0;
  }

  let failures = 0;
  let docId: string | null = null;
  o.onProgress?.(offset / o.file.size);
  while (offset < o.file.size || docId === null) {
    const end = Math.min(offset + CHUNK, o.file.size);
    try {
      const r = await patchChunk(url, offset, o.file.slice(offset, end), (n) => o.onProgress?.((offset + n) / o.file.size), o.signal);
      let problemBody: { title?: string; code?: string; errors?: FieldError[]; extra?: Record<string, unknown> } = {};
      if (r.status >= 400) {
        try {
          problemBody = JSON.parse(r.body);
        } catch {
          /* no body */
        }
      }
      if (r.status === 409 && problemBody.code === "offset_mismatch") {
        // We and the server disagree about the offset: ask, then carry on from there.
        const off = await headOffset(url, o.signal);
        if (off === null) throw new ApiError(404, "gone", "The upload expired. Try again.");
        offset = off;
        continue;
      }
      if (r.status >= 400) {
        const b = problemBody;
        remember(key, null);
        throw new ApiError(r.status, b.code || "error", b.title || `Upload failed (${r.status})`, b.errors || [], b.extra || {});
      }
      failures = 0;
      offset = Number(r.headers("Upload-Offset") ?? end);
      o.onProgress?.(offset / o.file.size);
      const id = r.headers("Docveta-Document-Id");
      if (id) docId = id;
      if (offset >= o.file.size && !docId) docId = ""; // finished without an id header: leave the loop, fail below
    } catch (e) {
      if ((e as Error).name === "AbortError") throw e;
      if (!(e instanceof ApiError) || e.status !== 0 || ++failures > RETRIES) throw e;
      // Connection dropped: wait, find out how much arrived, and resume.
      await sleep(Math.min(1000 * 2 ** failures, 15000), o.signal);
      const off = await headOffset(url, o.signal).catch(() => null);
      if (off !== null) offset = off;
    }
  }
  remember(key, null);
  if (!docId) throw new ApiError(0, "error", "The upload finished but the server didn't confirm it");
  return fetchDocument(docId);
}
