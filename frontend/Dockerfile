# Multi-stage build: Vite -> nginx static host + reverse proxy.
#
# Build-time args:
#   VITE_API_URL  Base URL the browser hits for API requests. The default
#                 ("/api") is a relative path that nginx proxies to the
#                 backend collector over the docker network.
#
# Runtime:
#   - nginx serves the built SPA on container port 80.
#   - /api/* is reverse-proxied to http://logging-collector:8080/* so
#     the browser only ever talks to a single origin (:7765 on the host)
#     and no CORS configuration is needed.

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

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
