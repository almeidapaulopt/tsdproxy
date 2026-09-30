
# Use an official Go image as the build base
FROM golang:1.27 AS builder
RUN apk add --no-cache ca-certificates && update-ca-certificates 2>/dev/null || true

# Set the working directory
WORKDIR /app

# Copy the source code into the container
COPY . .

# Build the Go application
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /tsdproxyd ./cmd/server/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /healthcheck ./cmd/healthcheck/main.go


FROM scratch

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

COPY --from=builder /tsdproxyd /tsdproxyd
COPY --from=builder /healthcheck /healthcheck

ENTRYPOINT ["/tsdproxyd"]

EXPOSE 8080
HEALTHCHECK CMD [ "/healthcheck" ]
