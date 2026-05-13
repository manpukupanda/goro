package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"goro/internal/config"
	"goro/internal/errcode"
	"goro/internal/queue"
	"goro/internal/referrer"

	"github.com/gin-gonic/gin"
)

// storageGetter is the subset of the S3 client used by the API server.
type storageGetter interface {
	GetObject(ctx context.Context, objectName string) (io.ReadCloser, int64, error)
	DeleteVideoObjects(ctx context.Context, publicID string) error
}

type Server struct {
	db          *sql.DB
	queue       *queue.Queue
	storage     storageGetter
	secureLink  config.SecureLinkConfig
	hlsConfig   config.HLSConfig
	tokenConfig config.PlaylistTokenConfig
	apiKey      string
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, errcode.FromMessage(message))
}

func NewServer(database *sql.DB, q *queue.Queue, s storageGetter, slCfg config.SecureLinkConfig, hlsCfg config.HLSConfig, tokenCfg config.PlaylistTokenConfig, apiKey string) *Server {
	return &Server{db: database, queue: q, storage: s, secureLink: slCfg, hlsConfig: hlsCfg, tokenConfig: tokenCfg, apiKey: apiKey}
}

func (s *Server) Router() *gin.Engine {
	r := gin.Default()

	// Unauthenticated routes.
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/videos/:id/playlist", s.getPlaylist)
	r.GET("/videos/:id/manifest", s.getManifest)
	r.GET("/hls/videos/:id/:profile/*asset", s.getHLSAsset)
	r.GET("/dash/videos/:id/:profile/*asset", s.getDASHAsset)

	// All routes below require a valid API key.
	auth := r.Group("/", s.requireAPIKey)
	auth.POST("/videos", s.uploadVideo)

	// Management endpoints
	auth.GET("/videos", s.listVideos)
	auth.PUT("/videos/:id/visibility", s.setVisibility)
	auth.PUT("/videos/:id/referrer-whitelist", s.setReferrerWhitelist)
	auth.POST("/videos/:id/tokens", s.issueToken)
	auth.DELETE("/videos/:id", s.deleteVideo)
	auth.GET("/videos/:id/download", s.downloadVideo)

	return r
}

// requireAPIKey is a Gin middleware that enforces API key authentication.
// The key must be supplied in the Authorization header as "Bearer <key>".
// Requests without a valid key receive 401 Unauthorized.
func (s *Server) requireAPIKey(c *gin.Context) {
	const prefix = "Bearer "
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, prefix) || strings.TrimPrefix(header, prefix) != s.apiKey {
		c.AbortWithStatusJSON(http.StatusUnauthorized, errcode.FromMessage("unauthorized"))
		return
	}
	c.Next()
}

func (s *Server) Start(addr string) {
	r := s.Router()
	log.Printf("API listening on %s", addr)
	r.Run(addr)
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
		PublicID          string   `json:"public_id"`
		OriginalName      string   `json:"original_name"`
		Status            string   `json:"status"`
		Visibility        string   `json:"visibility"`
		ReferrerWhitelist []string `json:"referrer_whitelist"`
		CreatedAt         string   `json:"created_at"`
		DurationSec       *float64 `json:"duration_sec,omitempty"`
		Width             *int     `json:"width,omitempty"`
		Height            *int     `json:"height,omitempty"`
		VideoCodec        *string  `json:"video_codec,omitempty"`
		Bitrate           *int64   `json:"bitrate,omitempty"`
		Framerate         *string  `json:"framerate,omitempty"`
		FramerateFloat    *float64 `json:"framerate_float,omitempty"`
		ContainerFormat   *string  `json:"container_format,omitempty"`
		AudioCodec        *string  `json:"audio_codec,omitempty"`
		AudioBitrate      *int64   `json:"audio_bitrate,omitempty"`
		SampleRate        *int     `json:"sample_rate,omitempty"`
		Channels          *int     `json:"channels,omitempty"`
		FileSize          *int64   `json:"file_size,omitempty"`
		AspectRatio       *string  `json:"aspect_ratio,omitempty"`
		Rotation          *int     `json:"rotation,omitempty"`
		HasAudio          *bool    `json:"has_audio,omitempty"`
		HasVideo          *bool    `json:"has_video,omitempty"`
	}

	base := `SELECT public_id, original_name, status, visibility, referrer_whitelist, created_at,
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
		writeError(c, http.StatusInternalServerError, "failed to query videos")
		return
	}
	defer rows.Close()

	videos := make([]videoRow, 0)
	for rows.Next() {
		var v videoRow
		var (
			durationSec       sql.NullFloat64
			width             sql.NullInt64
			height            sql.NullInt64
			videoCodec        sql.NullString
			bitrate           sql.NullInt64
			framerate         sql.NullString
			containerFormat   sql.NullString
			audioCodec        sql.NullString
			audioBitrate      sql.NullInt64
			sampleRate        sql.NullInt64
			channels          sql.NullInt64
			fileSize          sql.NullInt64
			aspectRatio       sql.NullString
			rotation          sql.NullInt64
			hasAudio          sql.NullInt64
			hasVideo          sql.NullInt64
			referrerWhitelist sql.NullString
		)
		if err := rows.Scan(
			&v.PublicID, &v.OriginalName, &v.Status, &v.Visibility, &referrerWhitelist, &v.CreatedAt,
			&durationSec, &width, &height, &videoCodec, &bitrate, &framerate,
			&containerFormat, &audioCodec, &audioBitrate, &sampleRate, &channels,
			&fileSize, &aspectRatio, &rotation, &hasAudio, &hasVideo,
		); err != nil {
			writeError(c, http.StatusInternalServerError, "failed to scan video")
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
		if referrerWhitelist.Valid {
			v.ReferrerWhitelist = referrer.DecodeWhitelist(referrerWhitelist.String)
		} else {
			v.ReferrerWhitelist = []string{}
		}
		videos = append(videos, v)
	}
	if err := rows.Err(); err != nil {
		writeError(c, http.StatusInternalServerError, "failed to iterate videos")
		return
	}

	c.JSON(http.StatusOK, gin.H{"videos": videos})
}

// parseRational converts a rational framerate string (e.g. "30000/1001") to a
// float64. Non-rational strings are parsed as plain floats.
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

func (s *Server) resolveProfile(requested string, allow func(config.ProfileFormat) bool) (config.HLSProfile, bool) {
	if requested != "" {
		for _, profile := range s.hlsConfig.Profiles {
			if profile.Name == requested && allow(profile.EffectiveFormat()) {
				return profile, true
			}
		}
		return config.HLSProfile{}, false
	}

	for _, profile := range s.hlsConfig.Profiles {
		if allow(profile.EffectiveFormat()) {
			return profile, true
		}
	}
	return config.HLSProfile{}, false
}

func (s *Server) uploadVideo(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, "file is required")
		return
	}
	if !strings.EqualFold(filepath.Ext(file.Filename), ".mp4") {
		writeError(c, http.StatusBadRequest, "only .mp4 is supported")
		return
	}

	tmpDir, err := os.MkdirTemp("", "goro-upload-")
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to prepare upload directory")
		return
	}
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := c.SaveUploadedFile(file, inputPath); err != nil {
		_ = os.RemoveAll(tmpDir)
		writeError(c, http.StatusInternalServerError, "failed to save uploaded file")
		return
	}

	publicID, err := s.queue.EnqueueVideo(file.Filename, inputPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		writeError(c, http.StatusInternalServerError, "failed to create video job")
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

	// Check visibility/referer when a DB is configured.
	if s.db != nil {
		if err := s.authorizePlayback(c, id); err != nil {
			return
		}
	}

	profile, ok := s.resolveProfile(c.Query("profile"), func(format config.ProfileFormat) bool {
		return format.IsHLS()
	})
	if !ok {
		writeError(c, http.StatusBadRequest, "valid hls profile is required")
		return
	}

	objectName := fmt.Sprintf("videos/%s/%s/index.m3u8", id, profile.Name)
	rc, _, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		writeError(c, http.StatusNotFound, "playlist not found")
		return
	}
	defer rc.Close()

	expires := time.Now().Unix() + int64(s.secureLink.TTLSec)
	out, err := rewritePlaylist(rc, id, profile.Name, expires, s.secureLink.Secret)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to process playlist")
		return
	}

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.String(http.StatusOK, out)
}

// getManifest fetches the DASH MPD for a video from storage, rewrites segment
// references with secure-link signed URLs, and returns the modified manifest.
func (s *Server) getManifest(c *gin.Context) {
	id := c.Param("id")

	if s.db != nil {
		if err := s.authorizePlayback(c, id); err != nil {
			return
		}
	}

	profile, ok := s.resolveProfile(c.Query("profile"), func(format config.ProfileFormat) bool {
		return format.IsDASH()
	})
	if !ok {
		writeError(c, http.StatusBadRequest, "valid dash profile is required")
		return
	}

	objectName := fmt.Sprintf("videos/%s/%s/index.mpd", id, profile.Name)
	rc, _, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		writeError(c, http.StatusNotFound, "manifest not found")
		return
	}
	defer rc.Close()

	expires := time.Now().Unix() + int64(s.secureLink.TTLSec)
	out, err := rewriteManifest(rc, id, profile.Name, expires, s.secureLink.Secret)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to process manifest")
		return
	}

	c.Header("Content-Type", "application/dash+xml")
	c.String(http.StatusOK, out)
}

// authorizePlayback checks whether the request is authorized to access the playlist/manifest.
// Returns nil if authorized, non-nil and writes a response if not.
func (s *Server) authorizePlayback(c *gin.Context, publicID string) error {
	var videoID int64
	var visibility string
	var rawWhitelist string
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT id, visibility, referrer_whitelist FROM videos WHERE public_id = ?`, publicID).Scan(&videoID, &visibility, &rawWhitelist)
	if err == sql.ErrNoRows {
		writeError(c, http.StatusNotFound, "video not found")
		return err
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to look up video")
		return err
	}

	whitelist := referrer.DecodeWhitelist(rawWhitelist)
	if !referrer.IsAllowed(c.GetHeader("Referer"), whitelist) {
		writeError(c, http.StatusForbidden, "referer is not allowed")
		return fmt.Errorf("referer is not allowed")
	}

	if visibility == "public" {
		return nil
	}

	// Private: validate token.
	token := c.Query("token")
	if token == "" {
		writeError(c, http.StatusForbidden, "token is required")
		return fmt.Errorf("missing token")
	}

	var expiresAtStr string
	err = s.db.QueryRowContext(c.Request.Context(),
		`SELECT expires_at FROM playlist_tokens WHERE token = ? AND video_id = ?`,
		token, videoID).Scan(&expiresAtStr)
	if err == sql.ErrNoRows {
		writeError(c, http.StatusForbidden, "invalid or expired token")
		return fmt.Errorf("token not found")
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to validate token")
		return err
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to parse token expiry")
		return err
	}
	if time.Now().After(expiresAt) {
		writeError(c, http.StatusForbidden, "invalid or expired token")
		return fmt.Errorf("token expired")
	}

	return nil
}

func (s *Server) authorizeStreamingAssetReferer(c *gin.Context, publicID string) error {
	var rawWhitelist string
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT referrer_whitelist FROM videos WHERE public_id = ?`, publicID).Scan(&rawWhitelist)
	if err == sql.ErrNoRows {
		writeError(c, http.StatusNotFound, "video not found")
		return err
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to look up video")
		return err
	}

	whitelist := referrer.DecodeWhitelist(rawWhitelist)
	if !referrer.IsAllowed(c.GetHeader("Referer"), whitelist) {
		writeError(c, http.StatusForbidden, "referer is not allowed")
		return fmt.Errorf("referer is not allowed")
	}
	return nil
}

// deleteVideo removes a video and all its associated data from the database and
// storage.  It deletes playlist_tokens and jobs first to satisfy foreign-key
// constraints, then deletes the videos row, and finally removes all objects
// from storage under videos/{id}/.
func (s *Server) deleteVideo(c *gin.Context) {
	publicID := c.Param("id")

	// Resolve the internal numeric ID and confirm the video exists.
	var videoID int64
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT id FROM videos WHERE public_id = ?`, publicID).Scan(&videoID)
	if err == sql.ErrNoRows {
		writeError(c, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to look up video")
		return
	}

	// Delete dependent rows atomically.
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(c.Request.Context(),
		`DELETE FROM playlist_tokens WHERE video_id = ?`, videoID); err != nil {
		writeError(c, http.StatusInternalServerError, "failed to delete tokens")
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(),
		`DELETE FROM jobs WHERE video_id = ?`, videoID); err != nil {
		writeError(c, http.StatusInternalServerError, "failed to delete jobs")
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(),
		`DELETE FROM videos WHERE id = ?`, videoID); err != nil {
		writeError(c, http.StatusInternalServerError, "failed to delete video")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(c, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	// Best-effort storage cleanup — log but do not fail the request.
	if err := s.storage.DeleteVideoObjects(c.Request.Context(), publicID); err != nil {
		log.Printf("deleteVideo: failed to remove storage objects for %s: %v", publicID, err)
	}

	c.Status(http.StatusNoContent)
}

// setVisibility updates the visibility of a video to either "public" or "private".
func (s *Server) setVisibility(c *gin.Context) {
	publicID := c.Param("id")
	var body struct {
		Visibility string `json:"visibility" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "visibility is required")
		return
	}
	if body.Visibility != "public" && body.Visibility != "private" {
		writeError(c, http.StatusBadRequest, "visibility must be 'public' or 'private'")
		return
	}

	res, err := s.db.ExecContext(c.Request.Context(),
		`UPDATE videos SET visibility = ? WHERE public_id = ?`, body.Visibility, publicID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to update visibility")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(c, http.StatusNotFound, "video not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"visibility": body.Visibility})
}

func (s *Server) setReferrerWhitelist(c *gin.Context) {
	publicID := c.Param("id")
	var body struct {
		ReferrerWhitelist []string `json:"referrer_whitelist"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "referrer whitelist must contain domains only")
		return
	}

	normalized, err := referrer.NormalizeWhitelist(body.ReferrerWhitelist)
	if err != nil {
		writeError(c, http.StatusBadRequest, "referrer whitelist must contain domains only")
		return
	}

	res, err := s.db.ExecContext(c.Request.Context(),
		`UPDATE videos SET referrer_whitelist = ? WHERE public_id = ?`, referrer.EncodeWhitelist(normalized), publicID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to update referrer whitelist")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(c, http.StatusNotFound, "video not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"referrer_whitelist": normalized})
}

// issueToken issues a short-lived opaque token for accessing a private video's playlist.
func (s *Server) issueToken(c *gin.Context) {
	publicID := c.Param("id")

	var videoID int64
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT id FROM videos WHERE public_id = ?`, publicID).Scan(&videoID)
	if err == sql.ErrNoRows {
		writeError(c, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to look up video")
		return
	}

	token, err := newToken(32)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to generate token")
		return
	}
	expiresAt := time.Now().Add(time.Duration(s.tokenConfig.TTLSec) * time.Second)

	_, err = s.db.ExecContext(c.Request.Context(),
		`INSERT INTO playlist_tokens (token, video_id, expires_at) VALUES (?, ?, ?)`,
		token, videoID, expiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to store token")
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token":      token,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

var extXMapURIRegexp = regexp.MustCompile(`URI="([^"]+)"`)

// rewritePlaylist reads an m3u8 from r and returns it with each segment and
// init reference replaced by a secure-link signed path.
func rewritePlaylist(r io.Reader, videoID, profile string, expires int64, secret string) (string, error) {
	var sb strings.Builder
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#EXT-X-MAP:"):
			match := extXMapURIRegexp.FindStringSubmatch(line)
			if len(match) == 2 {
				line = strings.Replace(line, match[1], secureAssetURL("/hls/videos", videoID, profile, match[1], expires, secret), 1)
			}
			sb.WriteString(line)
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			sb.WriteString(line)
		default:
			sb.WriteString(secureAssetURL("/hls/videos", videoID, profile, trimmed, expires, secret))
		}
		sb.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// rewriteManifest reads a DASH MPD and rewrites relative asset references to
// secure-link signed asset URLs.
func rewriteManifest(r io.Reader, videoID, profile string, expires int64, secret string) (string, error) {
	decoder := xml.NewDecoder(r)
	var buf bytes.Buffer
	encoder := xml.NewEncoder(&buf)
	inBaseURL := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "BaseURL" {
				inBaseURL = true
			}
			for i, attr := range t.Attr {
				switch attr.Name.Local {
				case "sourceURL", "media", "initialization", "href":
					t.Attr[i].Value = secureAssetURL("/dash/videos", videoID, profile, attr.Value, expires, secret)
				}
			}
			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
		case xml.EndElement:
			if t.Name.Local == "BaseURL" {
				inBaseURL = false
			}
			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
		case xml.CharData:
			if inBaseURL {
				t = xml.CharData([]byte(secureAssetURL("/dash/videos", videoID, profile, string(t), expires, secret)))
			}
			if err := encoder.EncodeToken(t); err != nil {
				return "", err
			}
		default:
			if err := encoder.EncodeToken(token); err != nil {
				return "", err
			}
		}
	}

	if err := encoder.Flush(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func secureAssetURL(routePrefix, videoID, profile, asset string, expires int64, secret string) string {
	trimmed := strings.TrimSpace(asset)
	if trimmed == "" ||
		trimmed == "." ||
		trimmed == "./" ||
		strings.HasPrefix(trimmed, "http://") ||
		strings.HasPrefix(trimmed, "https://") ||
		strings.HasPrefix(trimmed, "data:") ||
		strings.HasPrefix(trimmed, "/") {
		return asset
	}

	trimmed = strings.TrimPrefix(trimmed, "./")
	uri := fmt.Sprintf("%s/%s/%s/%s", routePrefix, videoID, profile, trimmed)
	sig := computeSecureLinkMD5(expires, uri, secret)
	return fmt.Sprintf("%s?expires=%d&md5=%s", uri, expires, sig)
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

// downloadVideo streams the original MP4 file from storage.
func (s *Server) downloadVideo(c *gin.Context) {
	publicID := c.Param("id")

	var originalName string
	err := s.db.QueryRowContext(c.Request.Context(),
		`SELECT original_name FROM videos WHERE public_id = ? AND status = 'ready'`, publicID).Scan(&originalName)
	if err == sql.ErrNoRows {
		writeError(c, http.StatusNotFound, "video not found or not ready")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to look up video")
		return
	}

	objectName := fmt.Sprintf("videos/%s/original.mp4", publicID)
	rc, size, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		writeError(c, http.StatusNotFound, "original file not found")
		return
	}
	defer rc.Close()

	asciiName := strings.Map(func(r rune) rune {
		if r > 127 || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, originalName)
	if asciiName == "" {
		asciiName = "video.mp4"
	}

	c.Header("Content-Type", "video/mp4")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, url.PathEscape(originalName)))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, rc); err != nil {
		log.Printf("error streaming mp4 %s: %v", objectName, err)
	}
}

// getHLSAsset streams a single HLS asset from storage. nginx performs the
// secure_link verification before proxying requests to this handler.
func (s *Server) getHLSAsset(c *gin.Context) {
	s.getStreamingAsset(c)
}

// getDASHAsset streams a single DASH asset from storage. nginx performs the
// secure_link verification before proxying requests to this handler.
func (s *Server) getDASHAsset(c *gin.Context) {
	s.getStreamingAsset(c)
}

func (s *Server) getStreamingAsset(c *gin.Context) {
	id := c.Param("id")
	profile := c.Param("profile")
	asset := strings.TrimPrefix(c.Param("asset"), "/")
	if asset == "" {
		writeError(c, http.StatusNotFound, "asset not found")
		return
	}
	if s.db != nil {
		if err := s.authorizeStreamingAssetReferer(c, id); err != nil {
			return
		}
	}

	objectName := fmt.Sprintf("videos/%s/%s/%s", id, profile, asset)
	rc, size, err := s.storage.GetObject(c.Request.Context(), objectName)
	if err != nil {
		writeError(c, http.StatusNotFound, "asset not found")
		return
	}
	defer rc.Close()

	c.Header("Content-Type", streamingContentType(asset))
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, rc); err != nil {
		log.Printf("error streaming asset %s: %v", objectName, err)
	}
}

func streamingContentType(name string) string {
	switch filepath.Ext(name) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".mpd":
		return "application/dash+xml"
	case ".m4s":
		return "video/iso.segment"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newToken returns a cryptographically random Base62 string of the given length.
// Rejection sampling is used to ensure a uniform distribution across all 62 characters.
func newToken(length int) (string, error) {
	// Accept bytes only in the range [0, maxAccept) to avoid modulo bias.
	// maxAccept is the largest multiple of len(base62Chars) that fits in a byte.
	const maxAccept = byte(len(base62Chars) * (256 / len(base62Chars)))
	b62 := make([]byte, 0, length)
	buf := make([]byte, length*2) // over-allocate to reduce re-reads
	for len(b62) < length {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < maxAccept {
				b62 = append(b62, base62Chars[int(b)%len(base62Chars)])
				if len(b62) == length {
					break
				}
			}
		}
	}
	return string(b62), nil
}
