package api

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
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
	db          *sql.DB
	queue       *queue.Queue
	storage     storageGetter
	secureLink  config.SecureLinkConfig
	hlsConfig   config.HLSConfig
	tokenConfig config.PlaylistTokenConfig
}

func NewServer(database *sql.DB, q *queue.Queue, s storageGetter, slCfg config.SecureLinkConfig, hlsCfg config.HLSConfig, tokenCfg config.PlaylistTokenConfig) *Server {
	return &Server{db: database, queue: q, storage: s, secureLink: slCfg, hlsConfig: hlsCfg, tokenConfig: tokenCfg}
}

func (s *Server) Router() *gin.Engine {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/videos", s.uploadVideo)
	r.GET("/videos/:id/playlist", s.getPlaylist)
	r.GET("/hls/videos/:id/:profile/:segment", s.getSegment)

	// Management endpoints
	r.PUT("/videos/:id/visibility", s.setVisibility)
	r.POST("/videos/:id/permissions", s.addPermission)
	r.DELETE("/videos/:id/permissions/:user_id", s.removePermission)
	r.POST("/videos/:id/tokens", s.issueToken)

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
// For private videos, a valid playlist token must be supplied via the ?token= query parameter.
func (s *Server) getPlaylist(c *gin.Context) {
	id := c.Param("id")

	// Check visibility when a DB is configured.
	if s.db != nil {
		if err := s.authorizePlaylist(c, id); err != nil {
			return
		}
	}

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

// authorizePlaylist checks whether the request is authorized to access the playlist.
// Returns nil if authorized (or video is public), non-nil and writes a response if not.
func (s *Server) authorizePlaylist(c *gin.Context, publicID string) error {
	var videoID int64
	var visibility string
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT id, visibility FROM videos WHERE public_id = ?`, publicID).Scan(&videoID, &visibility)
	if err == sql.ErrNoRows {
		// Video not found in DB yet (e.g., still processing) — allow access.
		return nil
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up video"})
		return err
	}

	if visibility == "public" {
		return nil
	}

	// Private: validate token.
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "token is required"})
		return fmt.Errorf("missing token")
	}

	var userID int64
	var expiresAtStr string
	err = s.db.QueryRowContext(c.Request.Context(),
		`SELECT user_id, expires_at FROM playlist_tokens WHERE token = ? AND video_id = ?`,
		token, videoID).Scan(&userID, &expiresAtStr)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired token"})
		return fmt.Errorf("token not found")
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate token"})
		return err
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse token expiry"})
		return err
	}
	if time.Now().After(expiresAt) {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired token"})
		return fmt.Errorf("token expired")
	}

	var exists int
	err = s.db.QueryRowContext(c.Request.Context(),
		`SELECT 1 FROM video_permissions WHERE video_id = ? AND user_id = ?`, videoID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "user does not have permission for this video"})
		return fmt.Errorf("permission denied")
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permission"})
		return err
	}

	return nil
}

// setVisibility updates the visibility of a video to either "public" or "private".
func (s *Server) setVisibility(c *gin.Context) {
	publicID := c.Param("id")
	var body struct {
		Visibility string `json:"visibility" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "visibility is required"})
		return
	}
	if body.Visibility != "public" && body.Visibility != "private" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "visibility must be 'public' or 'private'"})
		return
	}

	res, err := s.db.ExecContext(c.Request.Context(),
		`UPDATE videos SET visibility = ? WHERE public_id = ?`, body.Visibility, publicID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update visibility"})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"visibility": body.Visibility})
}

// addPermission grants a user permission to view a private video.
func (s *Server) addPermission(c *gin.Context) {
	publicID := c.Param("id")
	var body struct {
		UserID int64 `json:"user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	var videoID int64
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT id FROM videos WHERE public_id = ?`, publicID).Scan(&videoID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up video"})
		return
	}

	_, err = s.db.ExecContext(c.Request.Context(),
		`INSERT OR IGNORE INTO video_permissions (video_id, user_id) VALUES (?, ?)`, videoID, body.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add permission"})
		return
	}
	c.Status(http.StatusNoContent)
}

// removePermission revokes a user's permission to view a private video.
func (s *Server) removePermission(c *gin.Context) {
	publicID := c.Param("id")
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	var videoID int64
	err = s.db.QueryRowContext(c.Request.Context(),
		`SELECT id FROM videos WHERE public_id = ?`, publicID).Scan(&videoID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up video"})
		return
	}

	_, err = s.db.ExecContext(c.Request.Context(),
		`DELETE FROM video_permissions WHERE video_id = ? AND user_id = ?`, videoID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove permission"})
		return
	}
	c.Status(http.StatusNoContent)
}

// issueToken issues a short-lived opaque token for accessing a private video's playlist.
func (s *Server) issueToken(c *gin.Context) {
	publicID := c.Param("id")
	var body struct {
		UserID int64 `json:"user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	var videoID int64
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT id FROM videos WHERE public_id = ?`, publicID).Scan(&videoID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up video"})
		return
	}

	var exists int
	err = s.db.QueryRowContext(c.Request.Context(),
		`SELECT 1 FROM video_permissions WHERE video_id = ? AND user_id = ?`, videoID, body.UserID).Scan(&exists)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "user does not have permission for this video"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permission"})
		return
	}

	token, err := newToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}
	expiresAt := time.Now().Add(time.Duration(s.tokenConfig.TTLSec) * time.Second)

	_, err = s.db.ExecContext(c.Request.Context(),
		`INSERT INTO playlist_tokens (token, video_id, user_id, expires_at) VALUES (?, ?, ?, ?)`,
		token, videoID, body.UserID, expiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token":      token,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
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
//
// MD5 is used here because it is the algorithm mandated by the nginx
// ngx_http_secure_link_module; this function must produce output that matches
// nginx's own computation exactly.  The MD5 digest is not used as a
// general-purpose cryptographic hash — its sole purpose is URL-token
// verification in coordination with the nginx module.
func computeSecureLinkMD5(expires int64, uri, secret string) string {
	h := md5.Sum([]byte(fmt.Sprintf("%d%s%s", expires, uri, secret))) //nolint:gosec
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

const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newToken returns a cryptographically random Base62 string of the given length.
func newToken(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	b62 := make([]byte, length)
	for i, b := range buf {
		b62[i] = base62Chars[int(b)%len(base62Chars)]
	}
	return string(b62), nil
}
