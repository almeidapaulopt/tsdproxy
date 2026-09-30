# SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
# SPDX-License-Identifier: MIT

# Use an official Go image as the build base
FROM golang:1.27 AS builder
RUN apk add --no-cache ca-certificates && update-ca-certificates 2>/dev/null || true

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

ARG VERSION=0.0.0
ARG TAILSCALE_VERSION=0.0.0
ARG GIT_SHA=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="TSDproxy" \
      org.opencontainers.image.description="Temporary image based on v2.3.4 containing patched dependencies (especially tsnet)." \
      org.opencontainers.image.source="https://github.com/stephenrjr/tsdproxy" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.maintainer="stephenrjr@gmail.com"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /tsdproxyd /tsdproxyd

ENTRYPOINT ["/tsdproxyd"]
EXPOSE 8080
HEALTHCHECK --interval=1m --timeout=2s CMD [ "/tsdproxyd", "healthcheck" ]
