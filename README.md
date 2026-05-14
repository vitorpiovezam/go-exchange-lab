# go-exchange-lab

Simulação de plataforma de mercado financeiro com microserviços Go, Kafka, RabbitMQ e stack de observabilidade.

## Architecture

```
                        ┌─────────────────┐
                        │   market-api    │
                        │  (HTTP :8080)   │
                        └──┬──────────┬───┘
                           │          │
              Kafka        │          │        RabbitMQ
         price.updated     │          │      order.created
                           ▼          ▼
               ┌───────────────┐  ┌──────────────┐
               │   strategy-   │  │ order-worker  │
               │   consumer    │  │              │
               └───────────────┘  └──────────────┘
```

| Serviço | Função |
|---|---|
| **market-api** | API HTTP + produtor Kafka (preços) + publicador RabbitMQ (ordens) |
| **strategy-consumer** | Consome preços do Kafka e emite sinais de compra/venda |
| **order-worker** | Consome ordens do RabbitMQ e simula execução |

## Stack

- **Go 1.22** — microserviços
- **Apache Kafka** (KRaft) — streaming de eventos de preço
- **RabbitMQ** — fila de ordens com confirmação manual (Ack/Nack)
- **Grafana** — dashboards
- **Prometheus** — métricas (Kafka Exporter + RabbitMQ)
- **Loki + Promtail** — logs centralizados

## Quick Start

```bash
docker compose up --build
```

## Endpoints

| Método | URL | Descrição |
|---|---|---|
| GET | `http://localhost:8080/prices/latest` | Último preço PETR4 |
| POST | `http://localhost:8080/orders/market` | Criar ordem a mercado |

```bash
curl -X POST http://localhost:8080/orders/market \
  -H "Content-Type: application/json" \
  -d '{"symbol":"PETR4","side":"BUY","quantity":100}'
```

## UIs

| Serviço | URL | Credenciais |
|---|---|---|
| Grafana | http://localhost:3000 | admin / admin |
| Kafka UI | http://localhost:8081 | — |
| RabbitMQ | http://localhost:15672 | guest / guest |
| Prometheus | http://localhost:9090 | — |
