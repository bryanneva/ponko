package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bryanneva/ponko/internal/db"
	"github.com/bryanneva/ponko/internal/queue"
	"github.com/bryanneva/ponko/internal/worker"
	"github.com/riverqueue/river"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	pool, err := db.NewPool(ctx, "")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("Database connected")

	res, err := queue.RunMigrations(ctx, pool)
	if err != nil {
		pool.Close()
		log.Fatalf("Failed to run River migrations: %v", err)
	}
	log.Printf("River migrations complete (%d versions applied)", len(res.Versions))

	workers := river.NewWorkers()
	worker.RegisterPlaceholder(workers)

	concurrency := queue.WorkerConcurrency()
	client, err := queue.NewWithConcurrency(ctx, pool, workers, concurrency)
	if err != nil {
		pool.Close()
		log.Fatalf("Failed to start River client: %v", err)
	}
	log.Printf("River started (worker concurrency: %d)", concurrency)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("Listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	log.Println("HTTP server stopped")

	if err := client.Stop(shutdownCtx); err != nil {
		log.Printf("River client stop error: %v", err)
	}
	log.Println("River client stopped")

	pool.Close()
	log.Println("Database pool closed")
}
