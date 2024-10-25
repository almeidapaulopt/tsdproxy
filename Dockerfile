
# Usa uma imagem oficial do Go como base para a compilação
FROM golang:1.23 AS builder

# Define o diretório de trabalho
WORKDIR /app

# Copia o código fonte para o container
COPY . .

# Compila a aplicação Go
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /tsdproxyd ./cmd/server/main.go

# Usa uma imagem mínima para rodar a aplicação
FROM alpine:3.20
RUN apk --no-cache add curl

# Define o diretório de trabalho
WORKDIR /

# Copia o binário compilado para a nova imagem
COPY --from=builder /tsdproxyd /tsdproxyd

EXPOSE 8080

# HEALTHCHECK CMD wget -q http://localhost:8080/health/ready/ || exit 1

HEALTHCHECK CMD curl -f http://localhost:8080/health/ready/
# Executa o binário
ENTRYPOINT ["/tsdproxyd"]
