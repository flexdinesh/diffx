import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

export default defineConfig({
  build: { target: "safari16.4" },
  plugins: [react(), tailwindcss()],
});
