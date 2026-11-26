FROM node:24-bookworm-slim AS frontend
WORKDIR /ui
COPY ["frontend/package.json", "frontend/package-lock.json", "./"]
RUN npm ci --no-audit --no-fund
COPY ["frontend/", "./"]
RUN npm run lint && npm run test && npm run typecheck && npm run build

FROM golang:1.27.4-bookworm AS backend
WORKDIR /src
COPY ["backend/go.mod", "backend/go.sum", "./"]
RUN go mod download
COPY ["backend/", "./"]
COPY --from=frontend /ui/dist/ internal/handlers/web/
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/yihe ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/healthcheck ./cmd/healthcheck \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/protect-notes ./cmd/protect-notes

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 10001 yihe && useradd -r -u 10001 -g yihe yihe \
 && mkdir -p /app /data/images && chown -R yihe:yihe /app /data
COPY --from=backend /out/ /app/
USER 10001:10001
WORKDIR /app
ENV ADDR=0.0.0.0:8080 BLOB_BACKEND=mysql SESSION_BACKEND=database
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=4s --start-period=40s CMD ["/app/healthcheck"]
ENTRYPOINT ["/app/yihe"]
