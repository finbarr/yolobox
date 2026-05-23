import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  root: "app",
  build: {
    outDir: "../dist-app",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": {
        target: process.env.VITE_YOLOBOX_API_URL || "http://127.0.0.1:8787",
        changeOrigin: true,
      },
      "/healthz": {
        target: process.env.VITE_YOLOBOX_API_URL || "http://127.0.0.1:8787",
        changeOrigin: true,
      },
    },
  },
});
