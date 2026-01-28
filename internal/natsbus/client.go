package natsbus

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

type Client struct {
	nc *nats.Conn
	js nats.JetStreamContext
	kv nats.KeyValue
}

// Connect establishes NATS connection and initializes JetStream/KV.
func Connect() (*Client, error) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}

	opts := []nats.Option{
		nats.MaxReconnects(-1),
		nats.ReconnectWait(1 * time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		nats.ReconnectBufSize(8 * 1024 * 1024),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("WARN NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("INFO NATS reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Println("INFO NATS connection closed")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			log.Printf("ERROR NATS error: %v", err)
		}),
	}

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}
	log.Printf("INFO Connected to NATS at %s", nc.ConnectedUrl())

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create JetStream context: %w", err)
	}

	// Initialize infrastructure (streams, KV)
	if err := ensureInfrastructure(js); err != nil {
		nc.Close()
		return nil, fmt.Errorf("ensure infrastructure: %w", err)
	}

	// Bind to KV bucket
	kv, err := js.KeyValue("AGENTS")
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bind KV bucket: %w", err)
	}

	return &Client{nc: nc, js: js, kv: kv}, nil
}

// Close drains and closes the NATS connection.
func (c *Client) Close() error {
	return c.nc.Drain()
}

// NC returns the underlying NATS connection (for RPC).
func (c *Client) NC() *nats.Conn {
	return c.nc
}

// JS returns the JetStream context.
func (c *Client) JS() nats.JetStreamContext {
	return c.js
}

// KV returns the AGENTS KV bucket.
func (c *Client) KV() nats.KeyValue {
	return c.kv
}

func ensureInfrastructure(js nats.JetStreamContext) error {
	// Create OPS_EVENTS stream if not exists
	_, err := js.StreamInfo("OPS_EVENTS")
	if err == nats.ErrStreamNotFound {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:       "OPS_EVENTS",
			Subjects:   []string{"ops.*.events.>"},
			Retention:  nats.LimitsPolicy,
			MaxAge:     72 * time.Hour,
			MaxBytes:   10 * 1024 * 1024 * 1024, // 10GB
			MaxMsgSize: 1 * 1024 * 1024,         // 1MB
			Discard:    nats.DiscardOld,
			Storage:    nats.FileStorage,
		})
		if err != nil {
			return fmt.Errorf("create stream OPS_EVENTS: %w", err)
		}
		log.Println("INFO Created JetStream stream OPS_EVENTS")
	} else if err != nil {
		return fmt.Errorf("get stream info: %w", err)
	}

	// Create AGENTS KV bucket if not exists
	_, err = js.KeyValue("AGENTS")
	if err == nats.ErrBucketNotFound {
		_, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:       "AGENTS",
			TTL:          30 * time.Second,
			MaxValueSize: 8 * 1024,
			History:      1,
			Storage:      nats.FileStorage,
		})
		if err != nil {
			return fmt.Errorf("create KV bucket AGENTS: %w", err)
		}
		log.Println("INFO Created KV bucket AGENTS")
	} else if err != nil {
		return fmt.Errorf("get KV bucket: %w", err)
	}

	return nil
}
