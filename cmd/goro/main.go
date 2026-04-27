package main

import (
	"log"
	"os"

	"goro/internal/api"
	"goro/internal/config"
	"goro/internal/db"
	"goro/internal/queue"
	"goro/internal/storage"
	"goro/internal/worker"
)

func main() {
	cfgPath := os.Getenv("GORO_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	database, err := db.Open("data/goro.db")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}

	q := queue.New(database)

	s3, err := storage.New(cfg.S3)
	if err != nil {
		log.Fatalf("failed to initialize storage: %v", err)
	}

	// Start the configured number of worker goroutines. Keep this value low;
	// ffmpeg is CPU-intensive and excessive parallelism degrades encoding performance.
	for i := 0; i < cfg.Worker.Concurrency; i++ {
		go worker.Start(q, s3, cfg.HLS)
	}

	server := api.NewServer(database, q, s3, cfg.SecureLink, cfg.HLS, cfg.PlaylistToken)
	server.Start(":8080")
}
