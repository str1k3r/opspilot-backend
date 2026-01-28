package rpc

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"

	"opspilot-backend/internal/models"
)

var (
	ErrAgentOffline = errors.New("agent is offline")
	ErrTimeout      = errors.New("request timed out")
)

type Client struct {
	nc *nats.Conn
}

func NewClient(nc *nats.Conn) *Client {
	return &Client{nc: nc}
}

// ExecAction sends an action request to an agent and waits for response.
func (c *Client) ExecAction(agentID string, action string, args map[string]string, timeoutMS int) (*models.ActionResponseV3, error) {
	req := models.ActionRequestV3{
		Action:    action,
		Args:      args,
		RequestID: uuid.New().String(),
		TimeoutMS: timeoutMS,
	}

	payload, err := msgpack.Marshal(&req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	timeout := time.Duration(timeoutMS)*time.Millisecond + 5*time.Second
	if timeoutMS <= 0 {
		timeout = 15 * time.Second
	}
	if timeout > 125*time.Second {
		timeout = 125 * time.Second
	}

	subject := fmt.Sprintf("ops.%s.rpc", agentID)
	msg, err := c.nc.Request(subject, payload, timeout)
	if err != nil {
		if errors.Is(err, nats.ErrNoResponders) {
			return nil, ErrAgentOffline
		}
		if errors.Is(err, nats.ErrTimeout) {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("request: %w", err)
	}

	var resp models.ActionResponseV3
	if err := msgpack.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}
