package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"

	"opspilot-backend/internal/models"
	"opspilot-backend/internal/storage"
)

type InventoryConsumer struct {
	js      nats.JetStreamContext
	storage *storage.Storage
	sub     *nats.Subscription
}

func NewInventoryConsumer(js nats.JetStreamContext, storage *storage.Storage) *InventoryConsumer {
	return &InventoryConsumer{js: js, storage: storage}
}

// Start begins consuming inventory snapshots from JetStream.
func (c *InventoryConsumer) Start(ctx context.Context) error {
	sub, err := c.js.PullSubscribe(
		"ops.*.inventory",
		"backend-inventory",
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
	log.Println("INFO Inventory consumer started")
	return nil
}

func (c *InventoryConsumer) consumeLoop(ctx context.Context) {
	fetchSize := 64
	minFetch := 32
	maxFetch := 256
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
				log.Printf("WARN Inventory fetch error: %v", err)
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
				log.Printf("WARN Inventory process error: %v", err)
				msg.NakWithDelay(5 * time.Second)
				continue
			}
			msg.Ack()
		}
	}
}

func (c *InventoryConsumer) processMessage(msg *nats.Msg) error {
	var inv models.Inventory
	if err := msgpack.Unmarshal(msg.Data, &inv); err != nil {
		log.Printf("ERROR Inventory unmarshal error: %v", err)
		msg.Term()
		return nil
	}

	payload, err := json.Marshal(inv)
	if err != nil {
		return err
	}

	agentID, err := agentIDFromSubject(msg.Subject)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])

	if err := c.storage.InsertInventorySnapshot(agentID, hash, payload); err != nil {
		return err
	}

	log.Printf("INFO Inventory snapshot stored: agent=%s hash=%s", agentID, hash[:8])
	return nil
}

// Stop gracefully stops the consumer.
func (c *InventoryConsumer) Stop() error {
	if c.sub != nil {
		return c.sub.Drain()
	}
	return nil
}

func agentIDFromSubject(subject string) (string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("unexpected subject: %s", subject)
	}
	if parts[0] != "ops" || parts[2] != "inventory" {
		return "", fmt.Errorf("unexpected subject: %s", subject)
	}
	return parts[1], nil
}
