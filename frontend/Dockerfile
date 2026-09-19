# Multi-stage build: Vite -> static SPA + API proxy.
#
# Build-time args:
#   VITE_API_URL  Base URL the browser hits for API requests. Defaults
#                 to the relative path "/api" which the runtime `serve`
#                 config (serve.json) proxies to the backend collector.
#
# Runtime:
#   - `serve` hosts the built SPA on container port 3000.
#   - /api/* is reverse-proxied to http://logging-collector:8080/*, so the
#     browser only ever talks to a single origin (:7765 on the host) and
#     no CORS configuration is needed.

ARG VITE_API_URL=/api

FROM node:20-alpine AS build
WORKDIR /app

ARG VITE_API_URL
ENV VITE_API_URL=${VITE_API_URL}

COPY package.json ./
RUN npm install --no-audit --no-fund

COPY tsconfig.json vite.config.ts tailwind.config.ts postcss.config.js index.html ./
COPY src ./src

RUN npm run build

# Serve the built SPA with `serve`. SPA fallback (-s) is enabled so deep
# links / refreshes return index.html. The serve.json rewrites send any
# /api/* request to the backend collector over the docker network.
FROM node:20-alpine
RUN npm install -g serve@^14.2.4 --no-audit --no-fund

COPY --from=build /app/dist /app/dist
COPY serve.json /app/serve.json

WORKDIR /app
EXPOSE 3000
CMD ["serve", "-s", "dist", "-c", "/app/serve.json", "-l", "3000"]
