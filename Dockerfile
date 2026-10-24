FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /studio ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /monitor ./cmd/monitor
RUN go run ./cmd/licenses -out /licenses -binary /studio -binary /monitor

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg && rm -rf /var/lib/apt/lists/* && useradd --system --uid 10001 --create-home studio
WORKDIR /app
COPY --from=backend /studio /app/studio
COPY --from=backend /monitor /app/monitor
COPY --from=backend /licenses /app/licenses
COPY --from=web /src/web/dist /app/web
ENV WEB_DIR=/app/web LISTEN_ADDR=0.0.0.0:8090 FFPROBE_PATH=/usr/bin/ffprobe FFMPEG_PATH=/usr/bin/ffmpeg
USER studio
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=5s --start-period=75s --retries=3 \
  CMD ["curl", "--fail", "--silent", "--show-error", "--max-time", "3", "http://127.0.0.1:8090/healthz"]
ENTRYPOINT ["/app/studio"]
