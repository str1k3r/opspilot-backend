package workers

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"opspilot-backend/internal/cache"
	"opspilot-backend/internal/storage"
)

// StartHeartbeatReconciler periodically marks agents offline if last_seen key is missing.
func StartHeartbeatReconciler(ctx context.Context, cacheClient cache.Client, store *storage.Storage) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconcileOnce(cacheClient, store)
			}
		}
	}()
	log.Println("INFO Heartbeat reconciler started")
}

func reconcileOnce(cacheClient cache.Client, store *storage.Storage) {
	agentIDs, err := store.ListAgentIDs()
	if err != nil {
		log.Printf("WARN Heartbeat reconciler list agents error: %v", err)
		return
	}

	now := time.Now().Add(-2 * time.Minute)
	for _, agentID := range agentIDs {
		_, err := cacheClient.GetLastSeen(agentID)
		if err == redis.Nil {
			if err := store.MarkAgentOffline(agentID, now); err != nil {
				log.Printf("WARN Heartbeat reconciler mark offline error for %s: %v", agentID, err)
			}
			continue
		}
		if err != nil {
			log.Printf("WARN Heartbeat reconciler cache error for %s: %v", agentID, err)
		}
	}
}
