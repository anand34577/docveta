// Minimal typed fetch wrapper for the Docveta API. Errors are RFC 9457 problem details.

export interface FieldError {
  field: string;
  message: string;
}

export class ApiError extends Error {
  status: number;
  code: string;
  fields: FieldError[];
  extra: Record<string, unknown>;

  constructor(status: number, code: string, message: string, fields: FieldError[] = [], extra: Record<string, unknown> = {}) {
    super(message);
    this.status = status;
    this.code = code;
    this.fields = fields;
    this.extra = extra;
  }

  fieldError(field: string): string | undefined {
    return this.fields.find((f) => f.field === field)?.message;
  }
}

type Query = Record<string, string | number | boolean | string[] | undefined | null>;

export function toSearchParams(q: Query): URLSearchParams {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined || v === null || v === "" || v === false) continue;
    if (Array.isArray(v)) v.forEach((x) => p.append(k, x));
    else p.set(k, String(v));
  }
  return p;
}

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function parseError(res: Response): Promise<ApiError> {
  let body: { title?: string; code?: string; errors?: FieldError[]; extra?: Record<string, unknown> } = {};
  try {
    body = await res.json();
  } catch {
    /* not JSON */
  }
  const msg =
    body.title ||
    (res.status === 413
      ? "This file is too large"
      : res.status >= 500
        ? "The server had a problem. Please try again."
        : `Request failed (${res.status})`);
  return new ApiError(res.status, body.code || "error", msg, body.errors || [], body.extra || {});
}

export interface RequestOptions {
  query?: Query;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

export async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const url = "/api/v1" + path + (opts.query ? `?${toSearchParams(opts.query)}` : "");
  const headers: Record<string, string> = { Accept: "application/json", ...opts.headers };
  let body: BodyInit | undefined;
  if (opts.body instanceof FormData) {
    body = opts.body;
  } else if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  let res: Response;
  try {
    res = await fetch(url, { method, headers, body, credentials: "same-origin", signal: opts.signal });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError(0, "network", "Can't reach the server. Check your connection.");
  }
  if (res.status === 401 && onUnauthorized && !path.startsWith("/auth/")) onUnauthorized();
  if (!res.ok) throw await parseError(res);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  get: <T>(path: string, query?: Query, signal?: AbortSignal) => request<T>("GET", path, { query, signal }),
  post: <T>(path: string, body?: unknown, query?: Query) => request<T>("POST", path, { body, query }),
  put: <T>(path: string, body?: unknown) => request<T>("PUT", path, { body }),
  patch: <T>(path: string, body?: unknown, headers?: Record<string, string>) => request<T>("PATCH", path, { body, headers }),
  del: <T = void>(path: string, query?: Query) => request<T>("DELETE", path, { query }),
};

export interface UploadOptions {
  file: File;
  spaceId?: string;
  tagIds?: string[];
  allowDuplicate?: boolean;
  source?: string;
  onProgress?: (fraction: number) => void;
  signal?: AbortSignal;
}

/** Upload with progress (fetch has no upload progress, so this uses XHR). */
export function uploadDocument<T>(o: UploadOptions): Promise<T> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    // Metadata fields must come before the file: the server streams the file part.
    if (o.spaceId) form.append("space_id", o.spaceId);
    if (o.tagIds?.length) form.append("tag_ids", o.tagIds.join(","));
    if (o.allowDuplicate) form.append("allow_duplicate", "true");
    if (o.source) form.append("source", o.source);
    form.append("file", o.file, o.file.name);

    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/v1/documents");
    xhr.setRequestHeader("Accept", "application/json");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) o.onProgress?.(e.loaded / e.total);
    };
    xhr.onload = () => {
      let body: Record<string, unknown> = {};
      try {
        body = JSON.parse(xhr.responseText);
      } catch {
        /* ignore */
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as T);
      else
        reject(
          new ApiError(
            xhr.status,
            (body.code as string) || "error",
            (body.title as string) || (xhr.status === 413 ? "This file is too large" : `Upload failed (${xhr.status})`),
            (body.errors as FieldError[]) || [],
            (body.extra as Record<string, unknown>) || {},
          ),
        );
    };
    xhr.onerror = () => reject(new ApiError(0, "network", "Upload failed: can't reach the server"));
    xhr.onabort = () => reject(new DOMException("Aborted", "AbortError"));
    o.signal?.addEventListener("abort", () => xhr.abort());
    xhr.send(form);
  });
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return "Something went wrong";
}
