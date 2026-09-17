import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const apiUrl = env.VITE_API_URL || "http://localhost:4665";

  return {
    plugins: [react()],
    server: {
      host: "0.0.0.0",
      port: 5173,
      // Proxy /api -> backend so the browser sees a same-origin request
      // during dev. Same shape the app uses in prod (relative /api path),
      // so the API client doesn't need to know which env it's in.
      proxy: {
        "/api": {
          target: apiUrl,
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
