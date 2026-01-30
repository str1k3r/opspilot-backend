package ingest

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"

	"opspilot-backend/internal/models"
	"opspilot-backend/internal/storage"
)

type KVWatcher struct {
	kv      nats.KeyValue
	storage *storage.Storage
	watcher nats.KeyWatcher
}

func NewKVWatcher(kv nats.KeyValue, storage *storage.Storage) *KVWatcher {
	return &KVWatcher{kv: kv, storage: storage}
}

// Start begins watching the AGENTS KV bucket.
func (w *KVWatcher) Start(ctx context.Context) error {
	watcher, err := w.kv.WatchAll()
	if err != nil {
		return err
	}
	w.watcher = watcher

	go w.watchLoop(ctx)
	go w.reconcileLoop(ctx)

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
		var hb models.HeartbeatV3
		if err := msgpack.Unmarshal(entry.Value(), &hb); err != nil {
			log.Printf("ERROR KV unmarshal error for %s: %v", agentID, err)
			return
		}

		now := time.Now()
		agent := &models.Agent{
			ID:         uuid.New().String(),
			AgentID:    agentID,
			Hostname:   hb.Hostname,
			Status:     "online",
			LastSeenAt: &now,
		}

		if hb.Inventory != nil {
			if meta, err := json.Marshal(hb); err == nil {
				agent.Meta = meta
			}
		}

		if err := w.storage.CreateAgent(agent); err != nil {
			log.Printf("ERROR KV upsert agent error: %v", err)
			return
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

// reconcileLoop periodically marks stale agents as offline (TTL fallback).
func (w *KVWatcher) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.storage.MarkStaleAgentsOffline(90 * time.Second); err != nil {
				log.Printf("ERROR Reconcile error: %v", err)
			}
		}
	}
}

// Stop gracefully stops the watcher.
func (w *KVWatcher) Stop() error {
	if w.watcher != nil {
		return w.watcher.Stop()
	}
	return nil
}
