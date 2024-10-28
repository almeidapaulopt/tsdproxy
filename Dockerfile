
# Usa uma imagem oficial do Go como base para a compilação
FROM golang:1.23 AS builder
RUN apk add --no-cache ca-certificates && update-ca-certificates 2>/dev/null || true

# Define o diretório de trabalho
WORKDIR /app

# Copia o código fonte para o container
COPY . .

# Compila a aplicação Go
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /tsdproxyd ./cmd/server/main.go


FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /tsdproxyd /tsdproxyd

# add curl
COPY --from=ghcr.io/tarampampam/curl:8.10.1 /bin/curl /bin/curl

ENTRYPOINT ["/tsdproxyd"]

EXPOSE 8080
HEALTHCHECK CMD curl --fail http://127.0.0.1:8080/health/ready/
