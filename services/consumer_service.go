package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/everestp/deping-client-service/dto"
	amqp "github.com/rabbitmq/amqp091-go"
)

type ConsumerService interface {
	Start(ctx context.Context) error
	Stop()
}

type consumerService struct {
	conn         *amqp.Connection
	ch           *amqp.Channel
	alertService AlertService
	log          *slog.Logger
	queueName    string
}

func NewConsumerService(conn *amqp.Connection, alertService AlertService, log *slog.Logger) (ConsumerService, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}

	const (
		queueName    = "telegram_queue"
		exchangeName = "monitor_updates"
	)

	// 1. Ensure the Queue exists
	_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("declare queue: %w", err)
	}

	// 2. Ensure the Exchange exists (idempotent)
	err = ch.ExchangeDeclare(exchangeName, "fanout", true, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("declare exchange: %w", err)
	}

	// 3. FORCE BINDING: This guarantees the consumer receives messages
	err = ch.QueueBind(queueName, "", exchangeName, false, nil)
	if err != nil {
		return nil, fmt.Errorf("bind queue: %w", err)
	}

	// 4. Set QoS
	if err := ch.Qos(1, 0, false); err != nil {
		return nil, fmt.Errorf("set qos: %w", err)
	}

	return &consumerService{conn: conn, ch: ch, alertService: alertService, log: log, queueName: queueName}, nil
}

func (c *consumerService) Start(ctx context.Context) error {
	msgs, err := c.ch.Consume(c.queueName, "telegram-alert-consumer", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("start consume: %w", err)
	}

	c.log.Info("Telegram consumer ready and listening", "queue", c.queueName)

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-msgs:
			if !ok { return fmt.Errorf("channel closed") }
			c.handleDelivery(ctx, msg)
		}
	}
}
func (c *consumerService) Stop() {
	if c.ch != nil {
		_ = c.ch.Close()
	}
}

func (c *consumerService) handleDelivery(ctx context.Context, msg amqp.Delivery) {
    // 1. Log the arrival of a raw message
    c.log.Debug("received message from RabbitMQ", "body_len", len(msg.Body))

    var packet dto.SubmitResultsRequest
    if err := json.Unmarshal(msg.Body, &packet); err != nil {
        c.log.Error("failed to unmarshal message", "err", err, "raw_body", string(msg.Body))
        _ = msg.Nack(false, false)
        return
    }

    // 2. Log how many items are inside this batch
    c.log.Info("processing batch request", "results_count", len(packet.Results), "runner", packet.RunnerPubkey)

    if len(packet.Results) == 0 {
        c.log.Debug("empty batch received, acknowledging")
        _ = msg.Ack(false)
        return
    }

    processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    var lastErr error
    for _, result := range packet.Results {
        if result.MonitorID == "" {
            result.MonitorID = extractMonitorIDFromJobID(result.JobID)
        }

        // 3. Log before calling AlertService
        c.log.Debug("processing ping result", "monitor_id", result.MonitorID)

        if err := c.alertService.ProcessPingResult(processCtx, result); err != nil {
            c.log.Error("alert service failed to process result",
                "monitor_id", result.MonitorID,
                "err", err,
            )
            lastErr = err
        }
    }

    if lastErr != nil {
        c.log.Warn("batch processing completed with errors, requeueing message", "err", lastErr)
        _ = msg.Nack(false, true)
        return
    }

    // 4. Log successful completion
    c.log.Info("successfully processed and acknowledged batch")
    _ = msg.Ack(false)
}

func extractMonitorIDFromJobID(jobID string) string {
	for i, ch := range jobID {
		if ch == ':' {
			return jobID[:i]
		}
	}
	return jobID
}
