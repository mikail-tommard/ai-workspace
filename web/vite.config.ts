import { defineConfig } from "vite";
import path from "node:path";
export default defineConfig({
  root: path.resolve(import.meta.dirname),
  plugins: [
    {
      name: "embed-marker",
      generateBundle() {
        this.emitFile({ type: "asset", fileName: ".keep", source: "" });
      },
    },
  ],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
        configure(proxy) {
          proxy.on("proxyReq", (req) => req.removeHeader("origin"));
        },
      },
      "/ws": {
        target: "ws://127.0.0.1:8080",
        ws: true,
        changeOrigin: true,
        rewriteWsOrigin: true,
      },
    },
  },
});
