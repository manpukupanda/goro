package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"goro/internal/queue"

	"github.com/gin-gonic/gin"
)

type Server struct {
	queue *queue.Queue
}

func NewServer(q *queue.Queue) *Server {
	return &Server{queue: q}
}

func (s *Server) Router() *gin.Engine {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/videos", s.uploadVideo)

	return r
}

func (s *Server) Start(addr string) {
	r := s.Router()
	log.Printf("API listening on %s", addr)
	r.Run(addr)
}

func (s *Server) uploadVideo(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	if !strings.EqualFold(filepath.Ext(file.Filename), ".mp4") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .mp4 is supported"})
		return
	}

	tmpDir, err := os.MkdirTemp("", "goro-upload-")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare upload directory"})
		return
	}
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := c.SaveUploadedFile(file, inputPath); err != nil {
		_ = os.RemoveAll(tmpDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save uploaded file"})
		return
	}

	publicID, err := s.queue.EnqueueVideo(file.Filename, inputPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create video job"})
		return
	}

	log.Printf("queued video %s (%s)", publicID, file.Filename)
	c.JSON(http.StatusAccepted, gin.H{"video_id": publicID})
}
