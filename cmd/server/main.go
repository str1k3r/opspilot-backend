package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"opspilot-backend/internal/cache"
	"opspilot-backend/internal/handlers"
	"opspilot-backend/internal/ingest"
	"opspilot-backend/internal/natsbus"
	"opspilot-backend/internal/rpc"
	"opspilot-backend/internal/services"
	"opspilot-backend/internal/storage"
	"opspilot-backend/internal/workers"
)

func main() {
	if os.Getenv("JWT_SECRET") == "" {
		log.Fatal("JWT_SECRET is required")
	}

	// Database connection (with retries)
	var db *sqlx.DB
	var err error
	for i := 0; i < 10; i++ {
		db, err = sqlx.Connect("postgres", buildDSN())
		if err == nil {
			break
		}
		log.Printf("DB connection attempt %d failed: %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	log.Println("Connected to database")

	// NATS connection
	natsClient, err := natsbus.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer natsClient.Close()

	// Redis cache
	redisClient, err := cache.NewRedisClient()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisClient.Close()

	// Storage
	store := storage.NewStorage(db, redisClient)

	// RPC client
	rpcClient := rpc.NewClient(natsClient.NC())

	// Services
	aiClient := services.NewOpenRouterClient()
	slackClient := services.NewSlackClient()

	// Start consumers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsConsumer := ingest.NewEventsConsumer(natsClient.JS(), store)
	if err := eventsConsumer.Start(ctx); err != nil {
		log.Fatalf("Failed to start events consumer: %v", err)
	}

	inventoryConsumer := ingest.NewInventoryConsumer(natsClient.JS(), store)
	if err := inventoryConsumer.Start(ctx); err != nil {
		log.Fatalf("Failed to start inventory consumer: %v", err)
	}

	kvWatcher := ingest.NewKVWatcher(natsClient.KV(), store, redisClient)
	if err := kvWatcher.Start(ctx); err != nil {
		log.Fatalf("Failed to start KV watcher: %v", err)
	}

	keyEventsActive := workers.StartRedisKeyeventWorker(ctx, redisClient, store)
	if !keyEventsActive {
		log.Println("WARN Redis keyspace notifications are not active; fallback reconciler will be used")
		workers.StartHeartbeatReconciler(ctx, redisClient, store)
	}

	// HTTP handlers
	h := handlers.New(store, db, aiClient, slackClient, rpcClient, redisClient)

	// Router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	h.RegisterRoutes(r)

	server := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("Shutting down...")
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		_ = eventsConsumer.Stop()
		_ = inventoryConsumer.Stop()
		_ = kvWatcher.Stop()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Println("Server starting on :8080")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}

func buildDSN() string {
	return "host=" + getEnv("DB_HOST", "localhost") +
		" user=" + getEnv("DB_USER", "ops_user") +
		" password=" + getEnv("DB_PASSWORD", "ops_pass") +
		" dbname=" + getEnv("DB_NAME", "opspilot") +
		" sslmode=disable"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
