import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8787",
        changeOrigin: true,
        configure(proxy) {
          proxy.on("proxyReq", (request, incoming) => {
            // Preserve rejection of foreign origins at the application boundary.
            if (incoming.headers.origin === "http://127.0.0.1:5173")
              request.setHeader("Origin", "http://127.0.0.1:8787");
          });
        },
      },
    },
  },
  build: { target: "safari17", chunkSizeWarningLimit: 1400 },
  worker: { format: "es" },
});
