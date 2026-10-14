# syntax=docker/dockerfile:1.7
# Single production image: the Go API serves the Vite SPA from /srv, so one
# Render web service hosts UI + API on a single origin (no CORS, Lax cookies).
FROM node:24-alpine AS web-build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY frontend/ .
RUN npm run build

FROM golang:1.27.2-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ .
ARG VERSION=dev COMMIT=unknown
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT" \
    -o /out/quizforge ./cmd/api

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/quizforge /quizforge
COPY --from=web-build /app/dist /srv
ENV STATIC_DIR=/srv
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/quizforge"]
