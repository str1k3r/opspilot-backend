package workers

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"opspilot-backend/internal/cache"
	"opspilot-backend/internal/storage"
)

const lastSeenPrefix = "ops:agent:last_seen:"

// StartRedisKeyeventWorker subscribes to Redis key expiration events.
// Returns true when subscription is active.
func StartRedisKeyeventWorker(ctx context.Context, cacheClient cache.Client, store *storage.Storage) bool {
	pubsub, err := cacheClient.SubscribeExpired()
	if err != nil {
		log.Printf("WARN Redis keyevent subscribe failed: %v", err)
		return false
	}

	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok || msg == nil {
					return
				}
				handleExpired(cacheClient, store, msg)
			}
		}
	}()

	log.Println("INFO Redis keyevent worker started")
	return true
}

func handleExpired(cacheClient cache.Client, store *storage.Storage, msg *redis.Message) {
	if msg == nil {
		return
	}
	key := msg.Payload
	if !strings.HasPrefix(key, lastSeenPrefix) {
		return
	}
	agentID := strings.TrimPrefix(key, lastSeenPrefix)

	lastSeenMs, err := cacheClient.GetLastSeen(agentID)
	if err != nil {
		lastSeenMs = time.Now().Add(-2 * time.Minute).UnixMilli()
	}

	lastSeenAt := time.UnixMilli(lastSeenMs)
	if err := store.MarkAgentOffline(agentID, lastSeenAt); err != nil {
		log.Printf("WARN MarkAgentOffline failed for %s: %v", agentID, err)
		return
	}

	if err := cacheClient.SetStatus(agentID, "offline"); err != nil {
		log.Printf("WARN SetStatus offline failed for %s: %v", agentID, err)
	}
}
