import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { TanStackRouterVite } from "@tanstack/router-vite-plugin"
import { defineConfig } from "vite"

// https://vite.dev/config/
export default defineConfig({
  base: "/ui/",
  plugins: [TanStackRouterVite(), react(), tailwindcss()],
  build: {
    outDir: "../internal/server/webui",
    emptyOutDir: true,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    allowedHosts: true,
    proxy: {
      // Proxy Connect RPC requests to the backend server
      "/xagent.v1.XAgentService": {
        target: "http://localhost:6464",
        changeOrigin: true,
      },
      "/auth": {
        target: "http://localhost:6464",
        changeOrigin: true,
      }
    },
  },
})
