# syntax=docker/dockerfile:1.7
# Frontend static assets.
FROM node:24-alpine AS web-build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY frontend/ .
RUN npm run build

# Self-built Caddy v2.11.7 (S5-9): upstream 2.11.7-alpine still ships the Go
# toolchain flagged by Trivy (stdlib 1.26.8 / x/net 0.59.0). Pinned source tag +
# pinned Go toolchain + explicit x/net floor; Trivy re-verifies every release.
FROM golang:1.27.2-alpine AS caddy-build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
RUN git clone --depth 1 --branch v2.11.7 https://github.com/caddyserver/caddy.git .
RUN go get golang.org/x/net@v0.60.0
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/caddy ./cmd/caddy

FROM alpine:3.23
RUN apk add --no-cache ca-certificates mailcap
COPY --from=caddy-build /out/caddy /usr/bin/caddy
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
COPY --from=web-build /app/dist /srv
ENV XDG_CONFIG_HOME=/config XDG_DATA_HOME=/data
WORKDIR /srv
EXPOSE 80 443
ENTRYPOINT ["caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
