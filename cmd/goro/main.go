package main

import (
	"log"

	"goro/internal/api"
	"goro/internal/db"
	"goro/internal/queue"
	"goro/internal/storage"
	"goro/internal/worker"
)

func main() {
	// DB
	database, err := db.Open("data/goro.db")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}

	// Queue
	q := queue.New(database)

	// Storage (S3/MinIO)
	s3 := storage.New()

	// Worker (HLS / thumbnail)
	go worker.Start(q, s3)

	// API
	server := api.NewServer(q, s3)
	server.Start(":8080")
}
