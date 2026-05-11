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
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	DeleteVideoObjects(ctx context.Context, publicID string) error
}

// Server is the admin console HTTP server.
type Server struct {
	db              *sql.DB
	queue           *queue.Queue
	storage         storageAccessor
	hlsConfig       config.HLSConfig
	thumbnailConfig config.ThumbnailConfig
	secureLink      config.SecureLinkConfig
	tokenConfig     config.PlaylistTokenConfig
	credentials     gin.Accounts
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
	thumbnailCfg config.ThumbnailConfig,
) (*Server, error) {
	user := os.Getenv("GORO_ADMIN_USER")
	pass := os.Getenv("GORO_ADMIN_PASSWORD")
	if user == "" || pass == "" {
		return nil, fmt.Errorf("GORO_ADMIN_USER and GORO_ADMIN_PASSWORD must be set")
	}
	return &Server{
		db:              database,
		queue:           q,
		storage:         s,
		hlsConfig:       hlsCfg,
		thumbnailConfig: thumbnailCfg,
		secureLink:      slCfg,
		tokenConfig:     tokenCfg,
		credentials:     gin.Accounts{user: pass},
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
		api.DELETE("/videos/:id", s.deleteVideo)
		api.GET("/videos/:id/playlist", s.getPlaylist)
		api.GET("/videos/:id/download", s.downloadVideo)
		api.GET("/videos/:id/thumbnails/:name", s.getThumbnail)
		api.GET("/hls/:id/:profile/:segment", s.getSegment)
		api.GET("/jobs", s.listJobs)
		api.GET("/config", s.getConfig)
	}

	// Serve the SPA at /admin/ with fallback to index.html.
	sub, err := fs.Sub(uiDist, "ui/dist")
	if err != nil {
		log.Fatalf("admin: failed to sub ui/dist: %v", err)
	}
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
		// index.html is served directly to avoid http.FileServer's built-in
		// behaviour of redirecting "<dir>/index.html" requests to "<dir>/".
		serveFile := func(path string) {
			data, err := fs.ReadFile(sub, path)
			if err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			mime := "text/html; charset=utf-8"
			if strings.HasSuffix(path, ".js") {
				mime = "application/javascript"
			} else if strings.HasSuffix(path, ".css") {
				mime = "text/css"
			} else if strings.HasSuffix(path, ".svg") {
				mime = "image/svg+xml"
			} else if strings.HasSuffix(path, ".png") {
				mime = "image/png"
			} else if strings.HasSuffix(path, ".ico") {
				mime = "image/x-icon"
			}
			c.Data(http.StatusOK, mime, data)
		}

		if f, err := sub.Open(relPath); err == nil {
			f.Close()
			serveFile(relPath)
			return
		}

		// Fall back to index.html for SPA client-side routing.
		serveFile("index.html")
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

// listVideos returns videos ordered by created_at descending. The following
// optional query parameters filter the result set:
//
//   - name         – original_name LIKE %value%
//   - status       – exact match (queued / processing / ready / failed)
//   - visibility   – exact match (public / private)
//   - codec        – video_codec LIKE %value%
//   - duration_min – duration_sec >= value (seconds, float)
//   - duration_max – duration_sec <= value (seconds, float)
//   - width_min    – width >= value (pixels)
//   - width_max    – width <= value (pixels)
//   - height_min   – height >= value (pixels)
//   - height_max   – height <= value (pixels)
func (s *Server) listVideos(c *gin.Context) {
	type videoRow struct {
		PublicID        string   `json:"public_id"`
		OriginalName    string   `json:"original_name"`
		Status          string   `json:"status"`
		Visibility      string   `json:"visibility"`
		CreatedAt       string   `json:"created_at"`
		DurationSec     *float64 `json:"duration_sec,omitempty"`
		Width           *int     `json:"width,omitempty"`
		Height          *int     `json:"height,omitempty"`
		VideoCodec      *string  `json:"video_codec,omitempty"`
		Bitrate         *int64   `json:"bitrate,omitempty"`
		Framerate       *string  `json:"framerate,omitempty"`
		FramerateFloat  *float64 `json:"framerate_float,omitempty"`
		ContainerFormat *string  `json:"container_format,omitempty"`
		AudioCodec      *string  `json:"audio_codec,omitempty"`
		AudioBitrate    *int64   `json:"audio_bitrate,omitempty"`
		SampleRate      *int     `json:"sample_rate,omitempty"`
		Channels        *int     `json:"channels,omitempty"`
		FileSize        *int64   `json:"file_size,omitempty"`
		AspectRatio     *string  `json:"aspect_ratio,omitempty"`
		Rotation        *int     `json:"rotation,omitempty"`
		HasAudio        *bool    `json:"has_audio,omitempty"`
		HasVideo        *bool    `json:"has_video,omitempty"`
	}

	base := `SELECT public_id, original_name, status, visibility, created_at,
		duration_sec, width, height, video_codec, bitrate, framerate,
		container_format, audio_codec, audio_bitrate, sample_rate, channels,
		file_size, aspect_ratio, rotation, has_audio, has_video
		FROM videos`

	var conds []string
	var args []interface{}

	if v := c.Query("name"); v != "" {
		conds = append(conds, "original_name LIKE ?")
		args = append(args, "%"+v+"%")
	}
	if v := c.Query("status"); v != "" {
		conds = append(conds, "status = ?")
		args = append(args, v)
	}
	if v := c.Query("visibility"); v != "" {
		conds = append(conds, "visibility = ?")
		args = append(args, v)
	}
	if v := c.Query("codec"); v != "" {
		conds = append(conds, "video_codec LIKE ?")
		args = append(args, "%"+v+"%")
	}
	if v := c.Query("duration_min"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			conds = append(conds, "duration_sec >= ?")
			args = append(args, f)
		}
	}
	if v := c.Query("duration_max"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			conds = append(conds, "duration_sec <= ?")
			args = append(args, f)
		}
	}
	if v := c.Query("width_min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			conds = append(conds, "width >= ?")
			args = append(args, n)
		}
	}
	if v := c.Query("width_max"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			conds = append(conds, "width <= ?")
			args = append(args, n)
		}
	}
	if v := c.Query("height_min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			conds = append(conds, "height >= ?")
			args = append(args, n)
		}
	}
	if v := c.Query("height_max"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			conds = append(conds, "height <= ?")
			args = append(args, n)
		}
	}

	q := base
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(c.Request.Context(), q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query videos"})
		return
	}
	defer rows.Close()

	videos := make([]videoRow, 0)
	for rows.Next() {
		var v videoRow
		var (
			durationSec     sql.NullFloat64
			width           sql.NullInt64
			height          sql.NullInt64
			videoCodec      sql.NullString
			bitrate         sql.NullInt64
			framerate       sql.NullString
			containerFormat sql.NullString
			audioCodec      sql.NullString
			audioBitrate    sql.NullInt64
			sampleRate      sql.NullInt64
			channels        sql.NullInt64
			fileSize        sql.NullInt64
			aspectRatio     sql.NullString
			rotation        sql.NullInt64
			hasAudio        sql.NullInt64
			hasVideo        sql.NullInt64
		)
		if err := rows.Scan(
			&v.PublicID, &v.OriginalName, &v.Status, &v.Visibility, &v.CreatedAt,
			&durationSec, &width, &height, &videoCodec, &bitrate, &framerate,
			&containerFormat, &audioCodec, &audioBitrate, &sampleRate, &channels,
			&fileSize, &aspectRatio, &rotation, &hasAudio, &hasVideo,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan video"})
			return
		}
		if durationSec.Valid {
			v.DurationSec = &durationSec.Float64
		}
		if width.Valid {
			w := int(width.Int64)
			v.Width = &w
		}
		if height.Valid {
			h := int(height.Int64)
			v.Height = &h
		}
		if videoCodec.Valid {
			v.VideoCodec = &videoCodec.String
		}
		if bitrate.Valid {
			v.Bitrate = &bitrate.Int64
		}
		if framerate.Valid {
			v.Framerate = &framerate.String
			f := parseRational(framerate.String)
			v.FramerateFloat = &f
		}
		if containerFormat.Valid {
			v.ContainerFormat = &containerFormat.String
		}
		if audioCodec.Valid {
			v.AudioCodec = &audioCodec.String
		}
		if audioBitrate.Valid {
			v.AudioBitrate = &audioBitrate.Int64
		}
		if sampleRate.Valid {
			sr := int(sampleRate.Int64)
			v.SampleRate = &sr
		}
		if channels.Valid {
			ch := int(channels.Int64)
			v.Channels = &ch
		}
		if fileSize.Valid {
			v.FileSize = &fileSize.Int64
		}
		if aspectRatio.Valid {
			v.AspectRatio = &aspectRatio.String
		}
		if rotation.Valid {
			r := int(rotation.Int64)
			v.Rotation = &r
		}
		if hasAudio.Valid {
			ha := hasAudio.Int64 != 0
			v.HasAudio = &ha
		}
		if hasVideo.Valid {
			hv := hasVideo.Int64 != 0
			v.HasVideo = &hv
		}
		videos = append(videos, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to iterate videos"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"videos": videos})
}

// parseRational converts a rational framerate string (e.g. "30000/1001") to a
// float64 rounded to two decimal places. Non-rational strings are parsed as
// plain floats.
func parseRational(r string) float64 {
	parts := strings.SplitN(r, "/", 2)
	if len(parts) != 2 {
		f, _ := strconv.ParseFloat(r, 64)
		return f
	}
	num, _ := strconv.ParseFloat(parts[0], 64)
	den, _ := strconv.ParseFloat(parts[1], 64)
	if den == 0 {
		return 0
	}
	return num / den
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

// deleteVideo removes a video and all its associated data from the database and
// storage.
func (s *Server) deleteVideo(c *gin.Context) {
	publicID := c.Param("id")

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

	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to begin transaction"})
		return
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(c.Request.Context(),
		`DELETE FROM playlist_tokens WHERE video_id = ?`, videoID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete tokens"})
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(),
		`DELETE FROM jobs WHERE video_id = ?`, videoID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete jobs"})
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(),
		`DELETE FROM videos WHERE id = ?`, videoID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete video"})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit transaction"})
		return
	}

	if err := s.storage.DeleteVideoObjects(c.Request.Context(), publicID); err != nil {
		log.Printf("admin deleteVideo: failed to remove storage objects for %s: %v", publicID, err)
	}

	c.Status(http.StatusNoContent)
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

// downloadVideo streams the original MP4 file from storage.
func (s *Server) downloadVideo(c *gin.Context) {
	publicID := c.Param("id")

	var originalName string
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT original_name FROM videos WHERE public_id = ? AND status = 'ready'`, publicID).Scan(&originalName)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found or not ready"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up video"})
		return
	}

	objectName := fmt.Sprintf("videos/%s/original.mp4", publicID)
	rc, size, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "original file not found"})
		return
	}
	defer rc.Close()

	c.Header("Content-Type", "video/mp4")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="video.mp4"; filename*=UTF-8''%s`, url.PathEscape(originalName)))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, rc); err != nil {
		log.Printf("admin: error streaming mp4 %s: %v", objectName, err)
	}
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
	c.Header("Content-Length", strconv.FormatInt(size, 10))
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
// the profile selector in the video player, and the thumbnail spec names so
// the SPA knows which thumbnails to display.
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

	thumbNames := make([]string, 0, len(s.thumbnailConfig.Specs))
	for _, spec := range s.thumbnailConfig.Specs {
		thumbNames = append(thumbNames, spec.Name)
	}

	c.JSON(http.StatusOK, gin.H{"profiles": profiles, "thumbnail_specs": thumbNames})
}

// getThumbnail streams a thumbnail image from storage.
func (s *Server) getThumbnail(c *gin.Context) {
	id := c.Param("id")
	name := c.Param("name")

	objectName := fmt.Sprintf("videos/%s/thumbnails/%s.jpg", id, name)
	rc, size, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "thumbnail not found"})
		return
	}
	defer rc.Close()

	c.Header("Content-Type", "image/jpeg")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, rc); err != nil {
		log.Printf("admin: error streaming thumbnail %s: %v", objectName, err)
	}
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
