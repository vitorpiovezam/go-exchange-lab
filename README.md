# go-exchange-lab

Simulação de plataforma de mercado financeiro com microserviços Go, Kafka, RabbitMQ e stack de observabilidade.

## Architecture

```
  ┌──────────────────────────────────────────────────────────┐
  │                      market-api                          │
  │                      (HTTP :8080)                        │
  │                                                          │
  │  • Simula preço da PETR4 a cada 500ms                    │
  │  • PUBLICA preço no Kafka (tópico "price.updated")       │
  │  • Recebe ordens via POST /orders/market                 │
  │  • PUBLICA ordem no RabbitMQ (fila "order.created")      │
  └──────┬───────────────────────────────────┬───────────────┘
         │                                   │
         │  Kafka                            │  RabbitMQ
         │  tópico: price.updated            │  fila: order.created
         ▼                                   ▼
  ┌──────────────────────┐          ┌────────────────────────┐
  │  strategy-consumer   │          │     order-worker       │
  │                      │          │                        │
  │  • CONSOME preços    │          │  • CONSOME ordens      │
  │    do Kafka          │          │    do RabbitMQ         │
  │  • Detecta zona de   │          │  • Simula execução     │
  │    compra (<=R$38)   │          │    (sleep 1s)          │
  │    ou venda (>=R$42) │          │  • Confirma com Ack    │
  │  • Loga sinais       │          │    ou rejeita com Nack │
  └──────────────────────┘          └────────────────────────┘
```

### Fluxo de dados

1. **market-api** gera preços aleatórios da PETR4 (entre R$35–45) e **publica no Kafka** a cada 500ms
2. **strategy-consumer** **consome do Kafka** esses eventos e loga quando o preço atinge zona de compra ou venda
3. Quando alguém faz `POST /orders/market`, o **market-api** pega o preço atual e **publica a ordem no RabbitMQ**
4. **order-worker** **consome do RabbitMQ**, simula a execução da ordem, e confirma (Ack) ou rejeita (Nack)

### Por que Kafka para preços e RabbitMQ para ordens?

- **Kafka** → ideal para **streaming de eventos** (muitas mensagens por segundo, múltiplos consumidores lendo o mesmo tópico)
- **RabbitMQ** → ideal para **filas de trabalho** (cada mensagem é processada por um único worker, com confirmação manual)

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
