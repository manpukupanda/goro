package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

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
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	dbPath, err := resolveDBPath()
	if err != nil {
		log.Fatalf("failed to resolve db path: %v", err)
	}

	database, err := db.Open(dbPath)
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

	server := api.NewServer(database, q, s3, cfg.SecureLink, cfg.HLS, cfg.PlaylistToken)
	server.Start(fmt.Sprintf(":%d", *apiPort))
}

// resolveDBPath returns the path to the SQLite database file.
// If the GORO_DB_PATH environment variable is set, it is used directly.
// Otherwise the path defaults to $XDG_DATA_HOME/goro/goro.db, falling back to
// ~/.local/share/goro/goro.db when XDG_DATA_HOME is not set.
// The parent directory is created if it does not already exist.
func resolveDBPath() (string, error) {
	if p := os.Getenv("GORO_DB_PATH"); p != "" {
		return p, nil
	}

	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not determine home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".local", "share")
	}

	dir := filepath.Join(dataDir, "goro")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("could not create data directory %s: %w", dir, err)
	}

	return filepath.Join(dir, "goro.db"), nil
}

