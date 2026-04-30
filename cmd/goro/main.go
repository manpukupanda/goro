package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"goro/internal/admin"
	"goro/internal/api"
	"goro/internal/config"
	"goro/internal/db"
	"goro/internal/queue"
	"goro/internal/storage"
	"goro/internal/worker"
)

func main() {
	apiPort := flag.Int("api-port", 5600, "Port for the public API server")
	consolePort := flag.Int("console-port", 5601, "Port for the admin console server")
	enableConsole := flag.Bool("console", false, "Enable the admin management console")
	flag.Parse()

	cfgPath := os.Getenv("GORO_CONFIG")

	var cfg *config.Config
	var err error
	if cfgPath != "" {
		cfg, err = config.Load(cfgPath)
	} else {
		cfg, err = config.LoadDefault()
	}
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
		go worker.Start(q, s3, cfg.HLS, cfg.Thumbnails)
	}

	if *enableConsole {
		adminSrv, err := admin.NewServer(database, q, s3, cfg.HLS, cfg.SecureLink, cfg.PlaylistToken, cfg.Thumbnails)
		if err != nil {
			log.Fatalf("failed to initialize admin console: %v", err)
		}
		go adminSrv.Start(fmt.Sprintf(":%d", *consolePort))
	}

	server := api.NewServer(database, q, s3, cfg.SecureLink, cfg.HLS, cfg.PlaylistToken, cfg.APIKey)
	server.Start(fmt.Sprintf(":%d", *apiPort))
}

