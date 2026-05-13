# Estágio 1: compilação — usa a imagem oficial do Go com Alpine (leve)
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copia os arquivos de dependência primeiro para aproveitar cache de camadas do Docker.
# Se go.mod/go.sum não mudaram, o Docker reutiliza o cache do "go mod download".
COPY go.mod go.sum ./
RUN go mod download

# Copia o código-fonte e compila com CGO desabilitado (binário estático)
COPY . .
RUN CGO_ENABLED=0 go build -o /service .

# Estágio 2: imagem final — Alpine mínimo (~5MB) só com o binário compilado
FROM alpine:3.20

RUN apk --no-cache add ca-certificates

COPY --from=builder /service /service

EXPOSE 8080

CMD ["/service"]
