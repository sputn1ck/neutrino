(() => {
  const coiHeaders = {
    "Cross-Origin-Embedder-Policy": "require-corp",
    "Cross-Origin-Opener-Policy": "same-origin"
  };

  if (typeof window === "undefined") {
    self.addEventListener("install", () => self.skipWaiting());

    self.addEventListener("activate", (event) => {
      event.waitUntil(self.clients.claim());
    });

    self.addEventListener("fetch", (event) => {
      if (event.request.cache === "only-if-cached" &&
          event.request.mode !== "same-origin") {
        return;
      }

      event.respondWith(fetch(event.request).then((response) => {
        if (response.status === 0) {
          return response;
        }

        const headers = new Headers(response.headers);
        for (const [key, value] of Object.entries(coiHeaders)) {
          headers.set(key, value);
        }

        return new Response(response.body, {
          status: response.status,
          statusText: response.statusText,
          headers
        });
      }));
    });

    return;
  }

  if (window.crossOriginIsolated || !navigator.serviceWorker) {
    return;
  }

  const reloadKey = "neutrino-coi-serviceworker-reloaded";
  const reloadOnce = () => {
    if (sessionStorage.getItem(reloadKey)) {
      return;
    }

    sessionStorage.setItem(reloadKey, "1");
    window.location.reload();
  };

  navigator.serviceWorker.register("coi-serviceworker.js").then(() => {
    if (navigator.serviceWorker.controller) {
      reloadOnce();
      return;
    }

    navigator.serviceWorker.addEventListener("controllerchange", reloadOnce);
  }).catch((err) => {
    console.warn("cross-origin isolation service worker unavailable", err);
  });
})();
