package api

import (
	"log"
	"net/http"

	"goro/internal/queue"
	"goro/internal/storage"

	"github.com/gin-gonic/gin"
)

type Server struct {
	queue   *queue.Queue
	storage *storage.S3
}

func NewServer(q *queue.Queue, s *storage.S3) *Server {
	return &Server{queue: q, storage: s}
}

func (s *Server) Start(addr string) {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	log.Printf("API listening on %s", addr)
	r.Run(addr)
}
