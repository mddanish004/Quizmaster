# syntax=docker/dockerfile:1.7
FROM node:24-alpine AS web-build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY frontend/ .
RUN npm run build

FROM caddy:2.11.7-alpine
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
COPY --from=web-build /app/dist /srv
