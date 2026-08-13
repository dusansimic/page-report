import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The server serves everything from one origin, so in development the Vite dev
// server has to look like that origin too: it owns the page and proxies the
// API, auth and report paths to the Go server. Set base_url in config.yml to
// this dev server's URL (with dev: true) so cookies and redirects line up.
const backend = process.env.PR_DEV_BACKEND ?? "http://127.0.0.1:8080";
const proxied = ["/pagereport.v1.", "/auth", "/p", "/healthz"];

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "./src") },
  },
  server: {
    port: 5173,
    proxy: Object.fromEntries(
      proxied.map((p) => [p, { target: backend, changeOrigin: false }]),
    ),
  },
  build: {
    // Emitted into the directory web/app.go embeds.
    outDir: "dist",
    emptyOutDir: true,
  },
});
