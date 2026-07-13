import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import path from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const apiTarget = process.env.VF_API_TARGET || "http://127.0.0.1:8080";
const wsTarget = process.env.VF_WS_TARGET || "ws://127.0.0.1:8080";

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
      "/api": apiTarget,
      "/health": apiTarget,
      "/ready": apiTarget,
      "/ws": {
        target: wsTarget,
        ws: true
      },
      "/install.sh": apiTarget
    }
  },
  build: {
    outDir: "../internal/master/api/frontend_dist",
    emptyOutDir: true
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    testTimeout: 30000
  }
});
