package ingest

import (
	"context"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"

	"opspilot-backend/internal/cache"
	"opspilot-backend/internal/models"
	"opspilot-backend/internal/storage"
)

type KVWatcher struct {
	kv      nats.KeyValue
	storage *storage.Storage
	cache   cache.Client
	watcher nats.KeyWatcher
}

func NewKVWatcher(kv nats.KeyValue, storage *storage.Storage, cache cache.Client) *KVWatcher {
	return &KVWatcher{kv: kv, storage: storage, cache: cache}
}

// Start begins watching the AGENTS KV bucket.
func (w *KVWatcher) Start(ctx context.Context) error {
	watcher, err := w.kv.WatchAll()
	if err != nil {
		return err
	}
	w.watcher = watcher

	go w.watchLoop(ctx)

	log.Println("INFO KV watcher started")
	return nil
}

func (w *KVWatcher) watchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-w.watcher.Updates():
			if entry == nil {
				continue
			}
			w.handleEntry(entry)
		}
	}
}

func (w *KVWatcher) handleEntry(entry nats.KeyValueEntry) {
	agentID := entry.Key()

	switch entry.Operation() {
	case nats.KeyValuePut:
		var hb models.Heartbeat
		if err := msgpack.Unmarshal(entry.Value(), &hb); err != nil {
			log.Printf("ERROR KV unmarshal error for %s: %v", agentID, err)
			return
		}

		now := time.Now().UnixMilli()
		if err := w.cache.SetLastSeen(agentID, now, 150); err != nil {
			log.Printf("ERROR KV last_seen cache error: %v", err)
			return
		}
		if status, err := w.cache.GetStatus(agentID); err == nil && status == "offline" {
			if err := w.cache.SetStatus(agentID, "online"); err != nil {
				log.Printf("WARN KV status update error: %v", err)
			}
		}
		log.Printf("INFO Agent heartbeat: %s (%s) cpu=%.1f%% mem=%.1f%%",
			agentID, hb.Hostname, hb.CPUPercent, hb.MemPercent)

	case nats.KeyValueDelete:
		if err := w.storage.UpdateAgentStatus(agentID, "offline"); err != nil {
			log.Printf("ERROR KV delete agent error: %v", err)
			return
		}
		log.Printf("INFO Agent offline (graceful): %s", agentID)

	case nats.KeyValuePurge:
		log.Printf("INFO Agent purged: %s", agentID)
	}
}

// Stop gracefully stops the watcher.
func (w *KVWatcher) Stop() error {
	if w.watcher != nil {
		return w.watcher.Stop()
	}
	return nil
}
