import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dev-time proxy target. The browser's axios uses the relative "/api"
// path (see src/api/client.ts) so the request lands here; Vite then
// forwards it to the backend collector running on the host. Set
// VITE_DEV_API_TARGET to point at a different backend (LAN IP, remote
// dev box, etc.). Must be an absolute URL — http-proxy can't parse a
// relative path.
const DEV_TARGET = process.env.VITE_DEV_API_TARGET ?? "http://localhost:4665";

export default defineConfig(() => {
  return {
    plugins: [react()],
    server: {
      host: "0.0.0.0",
      port: 5173,
      proxy: {
        "/api": {
          target: DEV_TARGET,
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/api/, ""),
        },
      },
    },
    preview: {
      host: "0.0.0.0",
      port: 5173,
    },
  };
});
