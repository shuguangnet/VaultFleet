import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import path from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src")
    }
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    strictPort: true,
    allowedHosts: true,
    proxy: {
      "/api": "https://vf.933999.xyz",
      "/health": "https://vf.933999.xyz",
      "/ready": "https://vf.933999.xyz",
      "/ws": {
        target: "wss://vf.933999.xyz",
        ws: true
      },
      "/install.sh": "https://vf.933999.xyz"
    }
  },
  build: {
    outDir: "../internal/master/api/frontend_dist",
    emptyOutDir: true
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts"
  }
});
