import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Level colors (kept semantic so future dark mode is trivial)
        level: {
          debug: "#6366f1",
          info: "#0ea5e9",
          warn: "#f59e0b",
          error: "#ef4444",
          fatal: "#a855f7",
        },
      },
      fontFamily: {
        mono: [
          "ui-monospace",
          "SFMono-Regular",
          "Menlo",
          "Monaco",
          "Consolas",
          "monospace",
        ],
      },
    },
  },
  plugins: [],
} satisfies Config;
