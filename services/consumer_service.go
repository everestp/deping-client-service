package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/everestp/deping-client-service/dto"
)

// ═══════════════════════════════════════════════════════════════════════════
// ConsumerService Interface
// ═══════════════════════════════════════════════════════════════════════════

type ConsumerService interface {
	// Start begins consuming from RabbitMQ. Blocks until ctx is cancelled.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the consumer.
	Stop()
}

// ═══════════════════════════════════════════════════════════════════════════
// Implementation
// ═══════════════════════════════════════════════════════════════════════════

type consumerService struct {
	conn         *amqp.Connection
	ch           *amqp.Channel
	alertService AlertService
	log          *slog.Logger
	queueName    string
}

func NewConsumerService(
	conn *amqp.Connection,
	alertService AlertService,
	log *slog.Logger,
) (ConsumerService, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}

	// Declare queue idempotently — ensures it exists before consuming
	_, err = ch.QueueDeclare(
		"processing_queue",
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("declare queue: %w", err)
	}

	// Process one message at a time per consumer to preserve ordering per monitor
	if err := ch.Qos(1, 0, false); err != nil {
		return nil, fmt.Errorf("set qos: %w", err)
	}

	return &consumerService{
		conn:         conn,
		ch:           ch,
		alertService: alertService,
		log:          log,
		queueName:    "processing_queue",
	}, nil
}

func (c *consumerService) Start(ctx context.Context) error {
	msgs, err := c.ch.Consume(
		c.queueName,
		"uptime-alert-consumer", // consumer tag
		false,                   // auto-ack: false — we ack manually after processing
		false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("start consume: %w", err)
	}

	c.log.Info("RabbitMQ consumer started", "queue", c.queueName)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("consumer shutting down")
			return nil

		case msg, ok := <-msgs:
			if !ok {
				c.log.Warn("RabbitMQ channel closed; reconnect needed")
				return fmt.Errorf("channel closed")
			}
			c.handleDelivery(ctx, msg)
		}
	}
}

func (c *consumerService) Stop() {
	if c.ch != nil {
		_ = c.ch.Close()
	}
}

// handleDelivery parses a SubmitResultsRequest and processes each PingResultItem.
func (c *consumerService) handleDelivery(ctx context.Context, msg amqp.Delivery) {
	var packet dto.SubmitResultsRequest
	if err := json.Unmarshal(msg.Body, &packet); err != nil {
		c.log.Error("failed to unmarshal message", "err", err, "body", string(msg.Body))
		// Nack without requeue — bad message format won't improve on retry
		_ = msg.Nack(false, false)
		return
	}

	if len(packet.Results) == 0 {
		_ = msg.Ack(false)
		return
	}

	c.log.Debug("processing batch",
		"runner", packet.RunnerPubkey,
		"results", len(packet.Results),
	)

	processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var lastErr error
	for i := range packet.Results {
		result := packet.Results[i]

		// Normalise: MonitorID may arrive via job_id split fallback
		if result.MonitorID == "" {
			result.MonitorID = extractMonitorIDFromJobID(result.JobID)
		}

		if err := c.alertService.ProcessPingResult(processCtx, result); err != nil {
			c.log.Error("process ping result failed",
				"monitor_id", result.MonitorID,
				"err", err,
			)
			lastErr = err
		}
	}

	if lastErr != nil {
		// Requeue on transient errors (e.g. DB temporarily unavailable)
		_ = msg.Nack(false, true)
		return
	}

	_ = msg.Ack(false)
}

// extractMonitorIDFromJobID parses "monitor_id:runner_pubkey:timestamp" → monitor_id
func extractMonitorIDFromJobID(jobID string) string {
	for i, ch := range jobID {
		if ch == ':' {
			return jobID[:i]
		}
	}
	return jobID
}
