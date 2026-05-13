// strategy-consumer é o serviço que consome eventos de preço do Kafka
// e analisa se o ativo atingiu uma zona de interesse (compra ou venda).
//
// Fluxo:
//   1. Conecta ao Kafka como consumidor do tópico "price.updated"
//   2. Pertence ao consumer group "strategy-consumer" — permite escalar horizontalmente
//   3. A cada mensagem recebida, verifica se o preço está em zona de compra (<=38) ou venda (>=42)
//   4. Por enquanto, apenas loga sinais — em produção poderia disparar ordens automáticas

package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

// PriceUpdated representa o evento publicado pelo market-api no Kafka
type PriceUpdated struct {
	Symbol    string    `json:"symbol"`
	Price     float64   `json:"price"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	// Lê o endereço do Kafka — dentro do Docker usa "kafka:9092",
	// localmente usa "localhost:9092" como fallback
	kafkaBroker := getEnv("KAFKA_BROKER", "localhost:9092")

	// Cria um consumidor Kafka para o tópico "price.updated".
	// O GroupID faz com que o Kafka distribua as partições entre os membros do grupo.
	// Se subir 2 instâncias deste serviço com o mesmo GroupID, cada uma recebe metade das partições.
	kafkaReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaBroker},
		Topic:   "price.updated",
		GroupID: "strategy-consumer",
	})
	defer kafkaReader.Close()

	log.Println("strategy-consumer rodando")
	log.Println("observando eventos price.updated...")

	// Loop infinito de consumo — ReadMessage bloqueia até receber uma mensagem
	for {
		msg, err := kafkaReader.ReadMessage(context.Background())
		if err != nil {
			log.Println("falha ao ler mensagem do Kafka:", err)
			continue
		}

		// Desserializa o JSON da mensagem para a struct PriceUpdated
		var price PriceUpdated
		if err := json.Unmarshal(msg.Value, &price); err != nil {
			log.Println("evento de preço inválido:", err)
			continue
		}

		log.Printf("preço recebido: %s R$%.2f\n", price.Symbol, price.Price)

		// Zona de compra — preço baixo indica oportunidade de compra
		if price.Symbol == "PETR4" && price.Price <= 38 {
			log.Println("sinal: PETR4 está perto da zona de compra")
		}

		// Zona de venda — preço alto indica oportunidade de venda
		if price.Symbol == "PETR4" && price.Price >= 42 {
			log.Println("sinal: PETR4 está perto da zona de venda")
		}
	}
}

// getEnv retorna o valor da variável de ambiente ou o fallback se não estiver definida
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
