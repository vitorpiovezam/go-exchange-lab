// market-api é o serviço principal da aplicação.
// Ele simula um mercado financeiro publicando preços via Kafka
// e recebendo ordens de compra/venda via HTTP, encaminhando-as para o RabbitMQ.
//
// Fluxo:
//   1. Ao iniciar, conecta no Kafka (produtor) e no RabbitMQ (publicador)
//   2. Inicia uma goroutine que simula variação de preço da PETR4 a cada 500ms
//   3. Expõe dois endpoints HTTP:
//      - GET  /prices/latest  → retorna o último preço em memória
//      - POST /orders/market  → cria uma ordem de mercado e publica no RabbitMQ

package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/segmentio/kafka-go"
)

// PriceUpdated representa um evento de atualização de preço
// publicado no tópico Kafka "price.updated"
type PriceUpdated struct {
	Symbol    string    `json:"symbol"`
	Price     float64   `json:"price"`
	Timestamp time.Time `json:"timestamp"`
}

// CreateMarketOrderRequest é o corpo da requisição POST /orders/market
type CreateMarketOrderRequest struct {
	Symbol   string `json:"symbol"`
	Side     string `json:"side"`     // "BUY" ou "SELL"
	Quantity int    `json:"quantity"` // quantidade de ações
}

// OrderCreated representa a ordem que será publicada na fila RabbitMQ "order.created"
type OrderCreated struct {
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
	Reason   string  `json:"reason"`
}

// lastPrices armazena o último preço de cada ativo em memória.
// priceMutex protege o mapa contra acesso concorrente (goroutine de simulação + handlers HTTP).
var (
	lastPrices = map[string]float64{
		"PETR4": 38.50,
	}

	priceMutex sync.RWMutex
)

func main() {
	// Lê endereços de Kafka e RabbitMQ via variável de ambiente.
	// Dentro do Docker usa os nomes dos serviços (kafka:9092, rabbitmq:5672).
	// Em desenvolvimento local, usa localhost como fallback.
	kafkaBroker := getEnv("KAFKA_BROKER", "localhost:9092")
	rabbitmqURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")

	// Cria um produtor Kafka apontando para o tópico "price.updated".
	// Cada mensagem publicada aqui será consumida pelo strategy-consumer.
	kafkaWriter := kafka.NewWriter(kafka.WriterConfig{
		Brokers: []string{kafkaBroker},
		Topic:   "price.updated",
	})
	defer kafkaWriter.Close()

	// Conecta ao RabbitMQ — protocolo AMQP.
	// Essa conexão será usada para publicar ordens na fila "order.created".
	rabbitConn, err := amqp091.Dial(rabbitmqURL)
	if err != nil {
		log.Fatal("falha ao conectar no RabbitMQ:", err)
	}
	defer rabbitConn.Close()

	// Abre um canal AMQP dentro da conexão.
	// Canais são leves e permitem multiplexar operações sobre uma única conexão TCP.
	rabbitChannel, err := rabbitConn.Channel()
	if err != nil {
		log.Fatal("falha ao abrir canal RabbitMQ:", err)
	}
	defer rabbitChannel.Close()

	// Declara a fila "order.created" (idempotente — se já existir, não recria).
	// Parâmetros: durable=true (sobrevive restart), autoDelete=false, exclusive=false
	_, err = rabbitChannel.QueueDeclare(
		"order.created",
		true,  // durable — a fila persiste mesmo se o RabbitMQ reiniciar
		false, // autoDelete — não deleta quando todos os consumidores desconectam
		false, // exclusive — outros consumidores podem acessar
		false, // noWait — espera confirmação do servidor
		nil,   // args — sem argumentos extras (ex: TTL, DLQ)
	)
	if err != nil {
		log.Fatal("falha ao declarar fila de ordens:", err)
	}

	// Inicia a simulação de preço em uma goroutine separada.
	// Essa goroutine roda indefinidamente publicando preços no Kafka.
	go simulatePriceLoop(kafkaWriter, "PETR4")

	// GET /prices/latest — retorna o preço mais recente do ativo PETR4
	http.HandleFunc("/prices/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Leitura protegida por RLock — múltiplas goroutines podem ler simultaneamente
		priceMutex.RLock()
		price := lastPrices["PETR4"]
		priceMutex.RUnlock()

		response := PriceUpdated{
			Symbol:    "PETR4",
			Price:     price,
			Timestamp: time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// POST /orders/market — cria uma ordem a mercado e publica no RabbitMQ
	http.HandleFunc("/orders/market", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var input CreateMarketOrderRequest

		// Decodifica o JSON do corpo da requisição
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		// Validações básicas do request
		if input.Symbol == "" {
			http.Error(w, "symbol is required", http.StatusBadRequest)
			return
		}

		if input.Side != "BUY" && input.Side != "SELL" {
			http.Error(w, "side must be BUY or SELL", http.StatusBadRequest)
			return
		}

		if input.Quantity <= 0 {
			http.Error(w, "quantity must be greater than zero", http.StatusBadRequest)
			return
		}

		// Busca o preço atual do ativo para executar a ordem a mercado
		priceMutex.RLock()
		currentPrice, exists := lastPrices[input.Symbol]
		priceMutex.RUnlock()

		if !exists {
			http.Error(w, "symbol price not found", http.StatusNotFound)
			return
		}

		// Monta o evento de ordem criada
		order := OrderCreated{
			Symbol:   input.Symbol,
			Side:     input.Side,
			Quantity: input.Quantity,
			Price:    currentPrice,
			Reason:   "manual market order",
		}

		body, err := json.Marshal(order)
		if err != nil {
			http.Error(w, "failed to marshal order", http.StatusInternalServerError)
			return
		}

		// Publica a ordem na fila RabbitMQ "order.created".
		// O order-worker irá consumir essa mensagem e "executar" a ordem.
		err = rabbitChannel.Publish(
			"",              // exchange vazio — usa a default exchange do RabbitMQ
			"order.created", // routing key = nome da fila
			false,           // mandatory
			false,           // immediate
			amqp091.Publishing{
				ContentType:  "application/json",
				DeliveryMode: amqp091.Persistent, // mensagem persiste em disco
				Body:         body,
			},
		)

		if err != nil {
			log.Println("falha ao publicar ordem no RabbitMQ:", err)
			http.Error(w, "failed to publish order", http.StatusInternalServerError)
			return
		}

		log.Println("ordem de mercado publicada:", string(body))

		// Retorna 202 Accepted — a ordem foi enfileirada, não executada ainda
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(order)
	})

	log.Println("market-api rodando em http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// simulatePriceLoop simula variação de preço de um ativo em loop infinito.
// A cada 500ms aplica uma variação aleatória de até ±R$0,10 no preço,
// mantendo dentro dos limites [35, 45], e publica o novo preço no Kafka.
func simulatePriceLoop(writer *kafka.Writer, symbol string) {
	price := 38.50

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Variação aleatória entre -0.10 e +0.10
		variation := (rand.Float64() - 0.5) * 0.20
		price = price + variation

		// Limita o preço dentro de uma faixa realista
		if price < 35 {
			price = 35
		}

		if price > 45 {
			price = 45
		}

		price = round2(price)

		// Atualiza o mapa em memória (protegido por mutex)
		priceMutex.Lock()
		lastPrices[symbol] = price
		priceMutex.Unlock()

		event := PriceUpdated{
			Symbol:    symbol,
			Price:     price,
			Timestamp: time.Now().UTC(),
		}

		payload, err := json.Marshal(event)
		if err != nil {
			log.Println("falha ao serializar evento de preço:", err)
			continue
		}

		// Publica a mensagem no Kafka — a Key garante que todas as mensagens
		// do mesmo ativo vão para a mesma partição (ordenação garantida por ativo)
		err = writer.WriteMessages(context.Background(), kafka.Message{
			Key:   []byte(symbol),
			Value: payload,
		})

		if err != nil {
			log.Println("falha ao publicar preço no Kafka:", err)
			continue
		}

		log.Printf("preço publicado no Kafka: %s R$%.2f\n", symbol, price)
	}
}

// round2 arredonda um float para 2 casas decimais
func round2(value float64) float64 {
	return float64(int(value*100)) / 100
}

// getEnv retorna o valor da variável de ambiente ou o fallback se não estiver definida
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
