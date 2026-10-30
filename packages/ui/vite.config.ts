import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// Serves the development-only component gallery at /design.html.
export default defineConfig({
  plugins: [react(), tailwindcss()],
});
