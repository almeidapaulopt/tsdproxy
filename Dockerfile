# SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
# SPDX-License-Identifier: MIT

FROM --platform=$BUILDPLATFORM oven/bun:1 AS frontend
WORKDIR /app/web
COPY web/package.json web/bun.lock* ./
RUN bun install --frozen-lockfile
COPY web/ ./
COPY internal/ /app/internal/
RUN bun run build

FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG TAILSCALE_VERSION
ARG GIT_COMMIT

ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /app/web/dist ./web/dist

RUN go install github.com/a-h/templ/cmd/templ@latest && templ generate

# GOARM derived from TARGETVARIANT: v6→6, v7→7; empty for non-arm arches
RUN GOARM=${TARGETVARIANT#v} go build \
      -ldflags "-s -w \
        -X github.com/almeidapaulopt/tsdproxy/internal/core.version=${VERSION} \
        -X tailscale.com/version.Short=${TAILSCALE_VERSION} \
        -X tailscale.com/version.Long=${TAILSCALE_VERSION}-TSDProxy \
        -X tailscale.com/version.GitCommit=${GIT_COMMIT} \
        -X tailscale.com/version.shortStamp=${TAILSCALE_VERSION} \
        -X tailscale.com/version.longStamp=${TAILSCALE_VERSION}-TSDProxy \
        -X tailscale.com/version.gitCommitStamp=${GIT_COMMIT}" \
      -o /tsdproxyd ./cmd/server/main.go

RUN GOARM=${TARGETVARIANT#v} go build -ldflags "-s -w" -o /healthcheck ./cmd/healthcheck/main.go

FROM alpine:latest AS certs
RUN apk add --no-cache ca-certificates && update-ca-certificates 2>/dev/null || true

FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /tsdproxyd /tsdproxyd
COPY --from=builder /healthcheck /healthcheck

ENTRYPOINT ["/tsdproxyd"]
EXPOSE 8080
HEALTHCHECK --interval=1m --timeout=2s CMD [ "/healthcheck" ]
