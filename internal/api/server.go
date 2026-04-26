package api

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"goro/internal/config"
	"goro/internal/queue"

	"github.com/gin-gonic/gin"
)

// storageGetter is the subset of the S3 client used by the API server.
type storageGetter interface {
	GetObject(ctx context.Context, objectName string) (io.ReadCloser, int64, error)
}

type Server struct {
	queue      *queue.Queue
	storage    storageGetter
	secureLink config.SecureLinkConfig
	hlsConfig  config.HLSConfig
}

func NewServer(q *queue.Queue, s storageGetter, slCfg config.SecureLinkConfig, hlsCfg config.HLSConfig) *Server {
	return &Server{queue: q, storage: s, secureLink: slCfg, hlsConfig: hlsCfg}
}

func (s *Server) Router() *gin.Engine {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/videos", s.uploadVideo)
	r.GET("/videos/:id/playlist", s.getPlaylist)
	r.GET("/hls/videos/:id/:profile/:segment", s.getSegment)

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

// getPlaylist fetches the HLS playlist for a video from MinIO, rewrites each
// segment line with a secure-link signed URL, and returns the modified playlist.
func (s *Server) getPlaylist(c *gin.Context) {
	id := c.Param("id")

	profile := c.Query("profile")
	if profile == "" && len(s.hlsConfig.Profiles) > 0 {
		profile = s.hlsConfig.Profiles[0].Name
	}
	if profile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile query parameter is required"})
		return
	}

	objectName := fmt.Sprintf("videos/%s/%s/index.m3u8", id, profile)
	rc, _, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "playlist not found"})
		return
	}
	defer rc.Close()

	expires := time.Now().Unix() + int64(s.secureLink.TTLSec)
	out, err := rewritePlaylist(rc, id, profile, expires, s.secureLink.Secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process playlist"})
		return
	}

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.String(http.StatusOK, out)
}

// rewritePlaylist reads an m3u8 from r and returns it with each segment line
// replaced by a secure-link signed path.
func rewritePlaylist(r io.Reader, videoID, profile string, expires int64, secret string) (string, error) {
	var sb strings.Builder
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			sb.WriteString(line)
		} else {
			// Segment filename (e.g. segment000.ts)
			uri := fmt.Sprintf("/hls/videos/%s/%s/%s", videoID, profile, trimmed)
			sig := computeSecureLinkMD5(expires, uri, secret)
			sb.WriteString(fmt.Sprintf("%s?expires=%d&md5=%s", uri, expires, sig))
		}
		sb.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// computeSecureLinkMD5 returns the base64url-encoded (no padding) MD5 hash
// that nginx's secure_link_md5 directive produces for the formula:
//
//	md5("$secure_link_expires$uri$secret")
func computeSecureLinkMD5(expires int64, uri, secret string) string {
	h := md5.Sum([]byte(fmt.Sprintf("%d%s%s", expires, uri, secret)))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// getSegment streams a single .ts segment from MinIO.  nginx performs the
// secure_link verification before proxying requests to this handler.
func (s *Server) getSegment(c *gin.Context) {
	id := c.Param("id")
	profile := c.Param("profile")
	segment := c.Param("segment")

	objectName := fmt.Sprintf("videos/%s/%s/%s", id, profile, segment)
	rc, size, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "segment not found"})
		return
	}
	defer rc.Close()

	c.Header("Content-Type", "video/mp2t")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, rc); err != nil {
		log.Printf("error streaming segment %s: %v", objectName, err)
	}
}
