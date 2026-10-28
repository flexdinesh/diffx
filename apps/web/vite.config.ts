import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin, type ProxyOptions } from "vite";
import { startFixtureServer } from "./test/fixture-server.ts";

/** Serves the service worker source at its production path during dev. */
const serviceWorkerDevPath: Plugin = {
  name: "diffx-service-worker-dev",
  apply: "serve",
  configureServer(server) {
    server.middlewares.use((request, _response, next) => {
      if (request.url === "/sw.js") request.url = "/src/sw.ts";
      next();
    });
  },
};

export default defineConfig(async ({ command }) => {
  const configuredApiUrl = process.env.DIFFX_API_URL;
  const fixture =
    command === "serve" && !configuredApiUrl
      ? await startFixtureServer()
      : undefined;
  const apiUrl = configuredApiUrl ?? fixture?.target;
  const proxy: ProxyOptions | undefined = apiUrl
    ? {
        target: apiUrl,
        changeOrigin: true,
        configure(server) {
          server.on("proxyReq", (request) => request.removeHeader("Origin"));
        },
      }
    : undefined;
  return {
    server: {
      host: true,
      ...(proxy
        ? {
            proxy: {
              "/api": proxy,
              "/openapi.yaml": proxy,
            },
          }
        : {}),
    },
    build: {
      rolldownOptions: {
        input: {
          index: fileURLToPath(new URL("./index.html", import.meta.url)),
          sw: fileURLToPath(new URL("./src/sw.ts", import.meta.url)),
        },
        output: {
          // The service worker keeps a fixed root URL so its scope is the app.
          entryFileNames: (chunk: { name: string }) =>
            chunk.name === "sw" ? "sw.js" : "assets/[name]-[hash].js",
        },
      },
    },
    resolve: {
      alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
    },
    plugins: [
      react(),
      tailwindcss(),
      serviceWorkerDevPath,
      ...(fixture ? [fixture.plugin] : []),
    ],
  };
});
