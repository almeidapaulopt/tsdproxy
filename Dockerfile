
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
LABEL org.opencontainers.image.source="https://github.com/stephenrjr/tsdproxy"
LABEL maintainer="stephenrjr@gmail.com" \
      version="2.3.11" \
      description="Fresh image built to bump v2.3.4 dependencies (especilly tsnet)"


COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /tsdproxyd /tsdproxyd
COPY --from=builder /healthcheck /healthcheck

ENTRYPOINT ["/tsdproxyd"]

EXPOSE 8080
HEALTHCHECK CMD [ "/healthcheck" ]
