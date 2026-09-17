# Multi-stage build: Vite -> static SPA.
#
# Build-time args:
#   VITE_API_URL  Base URL the browser hits for API requests. Defaults
#                 to the backend collector exposed on host port 4665.
#
# Runtime: a tiny static file server hosts the built SPA on host port 7765.
# The browser talks to the backend directly (CORS to be added separately),
# so no reverse proxy is needed.

ARG VITE_API_URL=http://localhost:4665

FROM node:20-alpine AS build
WORKDIR /app

ARG VITE_API_URL
ENV VITE_API_URL=${VITE_API_URL}

COPY package.json ./
RUN npm install --no-audit --no-fund

COPY tsconfig.json vite.config.ts tailwind.config.ts postcss.config.js index.html ./
COPY src ./src

RUN npm run build

# Serve the built SPA with a minimal static server. `serve` handles the
# SPA fallback (unknown paths return index.html) out of the box.
FROM node:20-alpine
RUN npm install -g serve@^14.2.4 --no-audit --no-fund

COPY --from=build /app/dist /app/dist

WORKDIR /app
EXPOSE 3000
# `-s` enables SPA fallback so deep links / refreshes work.
CMD ["serve", "-s", "dist", "-l", "3000"]
