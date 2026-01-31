package ingest

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"

	"opspilot-backend/internal/models"
	"opspilot-backend/internal/storage"
)

type EventsConsumer struct {
	js      nats.JetStreamContext
	storage *storage.Storage
	sub     *nats.Subscription
}

func NewEventsConsumer(js nats.JetStreamContext, storage *storage.Storage) *EventsConsumer {
	return &EventsConsumer{js: js, storage: storage}
}

// Start begins consuming events from JetStream.
func (c *EventsConsumer) Start(ctx context.Context) error {
	sub, err := c.js.PullSubscribe(
		"ops.*.events.>",
		"backend-processor",
		nats.ManualAck(),
		nats.AckWait(30*time.Second),
		nats.MaxDeliver(3),
		nats.MaxAckPending(1000),
	)
	if err != nil {
		return err
	}
	c.sub = sub

	go c.consumeLoop(ctx)
	log.Println("INFO Events consumer started")
	return nil
}

func (c *EventsConsumer) consumeLoop(ctx context.Context) {
	fetchSize := 64
	minFetch := 8
	maxFetch := 512
	fullCount := 0
	emptyCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := c.sub.Fetch(fetchSize, nats.MaxWait(5*time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				log.Printf("WARN Fetch error: %v", err)
			}
			emptyCount++
			fullCount = 0
			if emptyCount >= 3 && fetchSize > minFetch {
				fetchSize /= 2
				if fetchSize < minFetch {
					fetchSize = minFetch
				}
				emptyCount = 0
			}
			continue
		}

		if len(msgs) == 0 {
			emptyCount++
			fullCount = 0
			if emptyCount >= 3 && fetchSize > minFetch {
				fetchSize /= 2
				if fetchSize < minFetch {
					fetchSize = minFetch
				}
				emptyCount = 0
			}
			continue
		}

		if len(msgs) == fetchSize {
			fullCount++
			emptyCount = 0
			if fullCount >= 3 && fetchSize < maxFetch {
				fetchSize *= 2
				if fetchSize > maxFetch {
					fetchSize = maxFetch
				}
				fullCount = 0
			}
		} else {
			fullCount = 0
			emptyCount = 0
		}

		for _, msg := range msgs {
			if err := c.processMessage(msg); err != nil {
				log.Printf("WARN Process error: %v", err)
				msg.NakWithDelay(5 * time.Second)
				continue
			}
			msg.Ack()
		}
	}
}

func (c *EventsConsumer) processMessage(msg *nats.Msg) error {
	var event models.Event
	if err := msgpack.Unmarshal(msg.Data, &event); err != nil {
		log.Printf("ERROR Unmarshal error (terminating): %v", err)
		msg.Term()
		return nil
	}

	log.Printf("INFO Event received: agent=%s type=%s", event.AgentID, event.AlertType)

	agent, err := c.storage.GetAgentByAgentID(event.AgentID)
	if err != nil {
		return err
	}
	if agent == nil {
		agent = &models.Agent{
			ID:       uuid.New().String(),
			AgentID:  event.AgentID,
			Hostname: "unknown",
			Status:   "online",
		}
		if err := c.storage.CreateAgent(agent); err != nil {
			return err
		}
	}

	source := event.GetSource()
	logs := event.GetLogs()

	rawError := event.Message
	if logs != "" {
		rawError = event.Message + "\n\n" + logs
	}
	rawError = strings.ReplaceAll(rawError, "\x00", "")

	contextJSON, _ := json.Marshal(event.Details)

	incident := &models.Incident{
		AgentID:     event.AgentID,
		Type:        event.AlertType,
		Source:      source,
		RawError:    rawError,
		ContextJSON: contextJSON,
		Status:      "new",
	}

	if err := c.storage.CreateIncident(incident); err != nil {
		return err
	}

	log.Printf("INFO Incident created: id=%d agent=%s type=%s source=%s",
		incident.ID, event.AgentID, event.AlertType, source)

	return nil
}

// Stop gracefully stops the consumer.
func (c *EventsConsumer) Stop() error {
	if c.sub != nil {
		return c.sub.Drain()
	}
	return nil
}
