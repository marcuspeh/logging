# syntax=docker/dockerfile:1.7

# ---- build stage ----
FROM golang:1.24-alpine AS build
WORKDIR /src

# Cache module downloads
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/logging-backend .

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/logging-backend /app/logging-backend

ENV HTTP_ADDR=:8080 \
    PARQUET_DIR=/data/parquet

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/logging-backend"]