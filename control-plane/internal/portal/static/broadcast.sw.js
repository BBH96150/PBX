// Minimal service worker for the Paging Broadcast PWA.
//
// Its job is installability + an app-shell cache so the console opens instantly
// and survives a flaky network. It deliberately does NOT cache POST /send
// (broadcast uploads) or the authenticated page HTML — those must always hit
// the network so auth + fresh group lists are correct.

const CACHE = 'paging-broadcast-v1';
const SHELL = [
  '/admin/static/broadcast.js',
  '/admin/static/broadcast-icon.svg',
  '/admin/broadcast.manifest.webmanifest',
];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  // Only handle GETs for our static shell; everything else goes to network.
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (!SHELL.includes(url.pathname)) return;
  e.respondWith(
    caches.match(req).then((hit) => hit || fetch(req).then((res) => {
      const copy = res.clone();
      caches.open(CACHE).then((c) => c.put(req, copy));
      return res;
    }))
  );
});
