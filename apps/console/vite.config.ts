import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig, type ProxyOptions } from "vite";

// Development proxies the API to a running diffx-console (mise run dev:console).
const proxy: ProxyOptions = {
  target: process.env.DIFFX_CONSOLE_URL ?? "http://127.0.0.1:7970",
  changeOrigin: true,
  configure(server) {
    server.on("proxyReq", (request) => request.removeHeader("Origin"));
  },
};

export default defineConfig({
  server: {
    host: true,
    proxy: { "/api": proxy, "/healthz": proxy, "/readyz": proxy },
  },
  plugins: [react(), tailwindcss()],
});
