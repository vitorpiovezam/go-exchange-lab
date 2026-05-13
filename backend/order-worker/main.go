// order-worker é o serviço que consome ordens da fila RabbitMQ "order.created"
// e simula a execução dessas ordens.
//
// Fluxo:
//   1. Conecta ao RabbitMQ e declara a fila "order.created" (idempotente)
//   2. Consome mensagens da fila com autoAck=false (confirmação manual)
//   3. Para cada mensagem:
//      - Desserializa a ordem
//      - Simula execução (sleep 1s)
//      - Envia Ack (confirmação) ao RabbitMQ se deu certo
//      - Envia Nack (rejeição sem reentrega) se a mensagem for inválida
//
// Em produção, o Nack sem reentrega mandaria a mensagem para uma DLQ (Dead Letter Queue)

package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

// OrderCreated representa a ordem publicada pelo market-api
type OrderCreated struct {
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
	Reason   string  `json:"reason"`
}

func main() {
	// Lê o endereço do RabbitMQ — dentro do Docker usa "rabbitmq:5672",
	// localmente usa "localhost:5672" como fallback
	rabbitmqURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")

	// Conecta ao RabbitMQ via AMQP
	conn, err := amqp091.Dial(rabbitmqURL)
	if err != nil {
		log.Fatal("falha ao conectar no RabbitMQ:", err)
	}
	defer conn.Close()

	// Abre um canal AMQP
	ch, err := conn.Channel()
	if err != nil {
		log.Fatal("falha ao abrir canal:", err)
	}
	defer ch.Close()

	// Declara a fila "order.created" — mesma declaração que o market-api faz.
	// Se a fila já existe com os mesmos parâmetros, não faz nada.
	// Se os parâmetros forem diferentes, o RabbitMQ retorna erro.
	queue, err := ch.QueueDeclare(
		"order.created",
		true,  // durable — sobrevive reinício do RabbitMQ
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,   // args
	)
	if err != nil {
		log.Fatal("falha ao declarar fila:", err)
	}

	// Começa a consumir mensagens da fila.
	// autoAck=false significa que nós controlamos quando confirmar a mensagem.
	// Isso garante que se o worker crashar durante o processamento,
	// a mensagem volta para a fila e outro worker pode processá-la.
	msgs, err := ch.Consume(
		queue.Name,
		"",    // consumer tag — vazio deixa o RabbitMQ gerar automaticamente
		false, // autoAck=false — confirmação manual via msg.Ack()
		false, // exclusive — permite múltiplos consumidores na mesma fila
		false, // noLocal — não usado no RabbitMQ
		false, // noWait — espera confirmação do servidor
		nil,   // args
	)
	if err != nil {
		log.Fatal("falha ao consumir fila:", err)
	}

	log.Println("order-worker rodando")
	log.Println("aguardando mensagens order.created...")

	// Loop infinito — range sobre o canal de mensagens do RabbitMQ
	for msg := range msgs {
		var order OrderCreated

		if err := json.Unmarshal(msg.Body, &order); err != nil {
			log.Println("ordem inválida:", err)

			// Nack sem reentrega — em produção iria para DLQ (Dead Letter Queue)
			msg.Nack(false, false)
			continue
		}

		log.Printf(
			"executando ordem: %s %d %s a R$%.2f | motivo: %s\n",
			order.Side,
			order.Quantity,
			order.Symbol,
			order.Price,
			order.Reason,
		)

		// Simula tempo de execução da ordem
		time.Sleep(1 * time.Second)

		log.Println("ordem executada com sucesso")

		// Ack confirma ao RabbitMQ que a mensagem foi processada.
		// Sem o Ack, a mensagem ficaria "unacked" e voltaria para a fila
		// se a conexão fosse perdida.
		msg.Ack(false)
	}
}

// getEnv retorna o valor da variável de ambiente ou o fallback se não estiver definida
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
