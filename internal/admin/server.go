package admin

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"goro/internal/config"
	"goro/internal/queue"

	"github.com/gin-gonic/gin"
)

//go:embed ui/dist
var uiDist embed.FS

// storageAccessor is the subset of S3 operations required by the admin server.
type storageAccessor interface {
	GetObject(ctx context.Context, objectName string) (io.ReadCloser, int64, error)
}

// Server is the admin console HTTP server.
type Server struct {
	db          *sql.DB
	queue       *queue.Queue
	storage     storageAccessor
	hlsConfig   config.HLSConfig
	secureLink  config.SecureLinkConfig
	tokenConfig config.PlaylistTokenConfig
	credentials gin.Accounts
}

// NewServer creates a new admin Server. It reads GORO_ADMIN_USER and
// GORO_ADMIN_PASSWORD from the environment and returns an error if either is
// not set.
func NewServer(
	database *sql.DB,
	q *queue.Queue,
	s storageAccessor,
	hlsCfg config.HLSConfig,
	slCfg config.SecureLinkConfig,
	tokenCfg config.PlaylistTokenConfig,
) (*Server, error) {
	user := os.Getenv("GORO_ADMIN_USER")
	pass := os.Getenv("GORO_ADMIN_PASSWORD")
	if user == "" || pass == "" {
		return nil, fmt.Errorf("GORO_ADMIN_USER and GORO_ADMIN_PASSWORD must be set")
	}
	return &Server{
		db:          database,
		queue:       q,
		storage:     s,
		hlsConfig:   hlsCfg,
		secureLink:  slCfg,
		tokenConfig: tokenCfg,
		credentials: gin.Accounts{user: pass},
	}, nil
}

// Router returns a configured Gin engine for the admin server.
func (s *Server) Router() *gin.Engine {
	r := gin.Default()

	// All /admin/api/* routes require Basic Auth.
	api := r.Group("/admin/api", gin.BasicAuth(s.credentials))
	{
		api.GET("/videos", s.listVideos)
		api.POST("/videos", s.uploadVideo)
		api.PUT("/videos/:id/visibility", s.setVisibility)
		api.GET("/videos/:id/playlist", s.getPlaylist)
		api.GET("/hls/:id/:profile/:segment", s.getSegment)
		api.GET("/jobs", s.listJobs)
		api.GET("/config", s.getConfig)
	}

	// Serve the SPA at /admin/ with fallback to index.html.
	sub, err := fs.Sub(uiDist, "ui/dist")
	if err != nil {
		log.Fatalf("admin: failed to sub ui/dist: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))

	// Gin does not allow registering a catch-all wildcard ("/admin/*path") on
	// the same router that already has named path segments under "/admin/api/".
	// Use NoRoute instead: unmatched requests that start with /admin or /admin/
	// are served as static SPA assets (or fall back to index.html for
	// client-side routing).
	// NOTE: Do NOT register r.GET("/admin", ...) here. Gin's default
	// RedirectTrailingSlash behaviour would redirect /admin/ → /admin because
	// that route exists, which would loop with any explicit /admin → /admin/
	// redirect we add ourselves.
	r.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if reqPath != "/admin" && !strings.HasPrefix(reqPath, "/admin/") {
			c.Status(http.StatusNotFound)
			return
		}

		// Resolve the path relative to the embedded dist root.
		// Both "/admin" and "/admin/" map to the SPA entry point.
		relPath := strings.TrimPrefix(reqPath, "/admin/")
		if relPath == "" || relPath == "/admin" {
			relPath = "index.html"
		}

		// Serve the asset if it exists in the embedded FS.
		if _, err := sub.Open(relPath); err == nil {
			c.Request.URL.Path = "/" + relPath
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		// Fall back to index.html for SPA client-side routing.
		c.Request.URL.Path = "/index.html"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	return r
}

// Start starts the admin HTTP server on the given address.
func (s *Server) Start(addr string) {
	r := s.Router()
	log.Printf("Admin console listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("admin console server error: %v", err)
	}
}

// listVideos returns all videos ordered by created_at descending.
func (s *Server) listVideos(c *gin.Context) {
	rows, err := s.db.QueryContext(c.Request.Context(),
		`SELECT public_id, original_name, status, visibility, created_at FROM videos ORDER BY created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query videos"})
		return
	}
	defer rows.Close()

	type videoRow struct {
		PublicID     string `json:"public_id"`
		OriginalName string `json:"original_name"`
		Status       string `json:"status"`
		Visibility   string `json:"visibility"`
		CreatedAt    string `json:"created_at"`
	}

	videos := make([]videoRow, 0)
	for rows.Next() {
		var v videoRow
		if err := rows.Scan(&v.PublicID, &v.OriginalName, &v.Status, &v.Visibility, &v.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan video"})
			return
		}
		videos = append(videos, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to iterate videos"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"videos": videos})
}

// uploadVideo saves an uploaded .mp4 to a temp dir and enqueues an encoding job.
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

	tmpDir, err := os.MkdirTemp("", "goro-admin-upload-")
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

	log.Printf("admin: queued video %s (%s)", publicID, file.Filename)
	c.JSON(http.StatusAccepted, gin.H{"video_id": publicID})
}

// setVisibility updates the visibility of a video.
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

// getPlaylist proxies the HLS playlist from storage (no secure-link rewriting;
// segments are also proxied through the admin API).
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

	// Rewrite segment lines to point to the admin HLS proxy endpoint.
	out, err := rewriteAdminPlaylist(rc, id, profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process playlist"})
		return
	}

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.String(http.StatusOK, out)
}

// getSegment streams a single .ts HLS segment directly from storage.
// This is used by the admin player which cannot access the nginx secure-link URLs.
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
	c.Header("Content-Length", fmt.Sprintf("%d", size))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, rc); err != nil {
		log.Printf("admin: error streaming segment %s: %v", objectName, err)
	}
}

// listJobs returns all jobs ordered by created_at descending.
func (s *Server) listJobs(c *gin.Context) {
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT j.id, v.public_id, j.status, j.created_at, j.updated_at, COALESCE(j.error_message, '')
FROM jobs j
JOIN videos v ON v.id = j.video_id
ORDER BY j.created_at DESC
`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query jobs"})
		return
	}
	defer rows.Close()

	type jobRow struct {
		ID            int64  `json:"id"`
		VideoPublicID string `json:"video_public_id"`
		Status        string `json:"status"`
		CreatedAt     string `json:"created_at"`
		UpdatedAt     string `json:"updated_at"`
		ErrorMessage  string `json:"error_message,omitempty"`
	}

	jobs := make([]jobRow, 0)
	for rows.Next() {
		var j jobRow
		if err := rows.Scan(&j.ID, &j.VideoPublicID, &j.Status, &j.CreatedAt, &j.UpdatedAt, &j.ErrorMessage); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan job"})
			return
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to iterate jobs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// getConfig returns the HLS profile configuration so the SPA can populate
// the profile selector in the video player.
func (s *Server) getConfig(c *gin.Context) {
	type profile struct {
		Name   string `json:"name"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	profiles := make([]profile, 0, len(s.hlsConfig.Profiles))
	for _, p := range s.hlsConfig.Profiles {
		profiles = append(profiles, profile{Name: p.Name, Width: p.Width, Height: p.Height})
	}
	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

// rewriteAdminPlaylist rewrites an m3u8 playlist so that each segment line
// references the admin proxy endpoint (/admin/api/hls/:id/:profile/:segment)
// rather than a bare filename, allowing hls.js to fetch segments through the
// admin API with Basic Auth.
func rewriteAdminPlaylist(r io.Reader, videoID, profile string) (string, error) {
	var sb strings.Builder
	all, err := io.ReadAll(io.LimitReader(r, 1<<20)) // 1 MiB limit
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(all), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			sb.WriteString(line)
		} else {
			sb.WriteString(fmt.Sprintf("/admin/api/hls/%s/%s/%s", videoID, profile, trimmed))
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}
