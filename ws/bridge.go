package ws

import (
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

// StartBridge creates a temporary queue, binds it to the monitor_updates exchange,
// and pipes every message into the Hub's Broadcast channel.
func StartBridge(hub *Hub, ch *amqp.Channel) {
	// 1. Declare a temporary, exclusive, auto-delete queue for this instance
	q, err := ch.QueueDeclare(
		"",    // name: let server choose a unique name
		false, // durable
		true,  // delete when unused
		true,  // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		log.Printf("[Bridge] failed to declare queue: %v", err)
		return
	}

	// 2. Bind to the exchange (assumes 'monitor_updates' exists as a fanout exchange)
	err = ch.QueueBind(
		q.Name,
		"",                // routing key (ignored for fanout)
		"monitor_updates", // exchange name
		false,
		nil,
	)
	if err != nil {
		log.Printf("[Bridge] failed to bind queue: %v", err)
		return
	}

	// 3. Start consuming messages from the queue
	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer tag
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Printf("[Bridge] failed to consume: %v", err)
		return
	}

	// 4. Pipe messages into the Hub
	go func() {
		log.Println("[Bridge] listening for monitor updates...")
		for d := range msgs {
			// Broadcast the body to all connected WebSocket clients
			hub.Broadcast <- d.Body
		}
	}()
}
