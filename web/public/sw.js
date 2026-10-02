// Docveta service worker: makes the app installable and receives files shared from
// Android's share sheet (Web Share Target) before the native app exists.
// Documents are never cached here: they're private and always fetched fresh.
const SHARE_CACHE = "docveta-share-v1";

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (event.request.method === "POST" && url.pathname === "/share-target") {
    event.respondWith(receiveShare(event.request));
  }
});

async function receiveShare(request) {
  try {
    const form = await request.formData();
    const files = form.getAll("files").filter((f) => f && typeof f !== "string");
    const cache = await caches.open(SHARE_CACHE);
    let i = 0;
    for (const f of files) {
      const headers = { "Content-Type": f.type || "application/octet-stream", "X-Filename": encodeURIComponent(f.name || `shared-${i}`) };
      await cache.put(`/__share/${Date.now()}-${i++}`, new Response(f, { headers }));
    }
  } catch (e) {
    // fall through: the app shows nothing to upload
  }
  return Response.redirect("/?shared=1", 303);
}
