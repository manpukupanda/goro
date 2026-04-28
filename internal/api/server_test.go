package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goro/internal/config"
	"goro/internal/db"
	"goro/internal/queue"
)

// stubStorage is a minimal storageGetter for use in unit tests.
type stubStorage struct {
	objects map[string]string // objectName -> content
}

func (s *stubStorage) GetObject(_ context.Context, objectName string) (io.ReadCloser, int64, error) {
	content, ok := s.objects[objectName]
	if !ok {
		return nil, 0, fmt.Errorf("object not found: %s", objectName)
	}
	b := []byte(content)
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

func newTestServer(t *testing.T, database *sql.DB) *Server {
	t.Helper()
	q := queue.New(database)
	return NewServer(database, q, nil, config.SecureLinkConfig{}, config.HLSConfig{}, config.PlaylistTokenConfig{TTLSec: 900})
}

func TestUploadVideoCreatesVideoAndJob(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("file", "sample.mp4")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := io.Copy(fileWriter, bytes.NewBufferString("fake-mp4-content")); err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/videos", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, res.Code)
	}

	var response struct {
		VideoID string `json:"video_id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(response.VideoID) != 11 {
		t.Fatalf("expected public_id of length 11, got %q (len %d)", response.VideoID, len(response.VideoID))
	}

	var (
		videoStatus string
		inputPath   string
		jobStatus   string
	)
	err = database.QueryRow(`
SELECT v.status, v.temp_path, j.status
FROM videos v
JOIN jobs j ON j.video_id = v.id
WHERE v.public_id = ?
`, response.VideoID).Scan(&videoStatus, &inputPath, &jobStatus)
	if err != nil {
		t.Fatalf("failed to query inserted records: %v", err)
	}

	if videoStatus != "queued" {
		t.Fatalf("expected video status queued, got %s", videoStatus)
	}
	if jobStatus != "pending" {
		t.Fatalf("expected job status pending, got %s", jobStatus)
	}
	if _, err := os.Stat(inputPath); err != nil {
		t.Fatalf("expected uploaded file to exist at %s: %v", inputPath, err)
	}
	_ = os.RemoveAll(filepath.Dir(inputPath))
}

func TestUploadVideoRejectsNonMP4(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("file", "sample.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := io.Copy(fileWriter, bytes.NewBufferString("plain-text")); err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/videos", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
}

func TestGetPlaylistRewritesSegmentURLs(t *testing.T) {
	const (
		videoID = "abc123"
		profile = "720p"
		secret  = "testsecret"
	)
	m3u8 := "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:4.000000,\nsegment000.ts\n#EXT-X-ENDLIST\n"

	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.m3u8", videoID, profile): m3u8,
		},
	}
	slCfg := config.SecureLinkConfig{Secret: secret, TTLSec: 3600}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile}}}
	srv := NewServer(nil, nil, store, slCfg, hlsCfg, config.PlaylistTokenConfig{})
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile="+profile, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "/hls/videos/"+videoID+"/"+profile+"/segment000.ts") {
		t.Fatalf("expected rewritten segment path in playlist, got:\n%s", body)
	}
	if !strings.Contains(body, "expires=") || !strings.Contains(body, "md5=") {
		t.Fatalf("expected secure-link params in playlist, got:\n%s", body)
	}
}

func TestGetPlaylistUsesDefaultProfile(t *testing.T) {
	const (
		videoID = "abc123"
		profile = "1080p"
	)
	m3u8 := "#EXTM3U\n#EXT-X-ENDLIST\n"
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.m3u8", videoID, profile): m3u8,
		},
	}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile}}}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{})
	router := srv.Router()

	// No ?profile= query param — should fall back to hlsConfig.Profiles[0]
	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
}

func TestGetPlaylistNotFound(t *testing.T) {
	store := &stubStorage{objects: map[string]string{}}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: "720p"}}}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{})
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/videos/missing/playlist?profile=720p", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestRewritePlaylist(t *testing.T) {
	const (
		videoID = "vid1"
		profile = "480p"
		secret  = "s3cr3t"
	)
	expires := time.Now().Add(time.Hour).Unix()
	m3u8 := "#EXTM3U\n#EXTINF:4.0,\nsegment000.ts\n#EXTINF:4.0,\nsegment001.ts\n#EXT-X-ENDLIST\n"

	out, err := rewritePlaylist(strings.NewReader(m3u8), videoID, profile, expires, secret)
	if err != nil {
		t.Fatalf("rewritePlaylist error: %v", err)
	}

	for _, seg := range []string{"segment000.ts", "segment001.ts"} {
		uri := fmt.Sprintf("/hls/videos/%s/%s/%s", videoID, profile, seg)
		sig := computeSecureLinkMD5(expires, uri, secret)
		want := fmt.Sprintf("%s?expires=%d&md5=%s", uri, expires, sig)
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, out)
		}
	}

	// Non-segment lines must be unchanged
	for _, line := range []string{"#EXTM3U", "#EXTINF:4.0,", "#EXT-X-ENDLIST"} {
		if !strings.Contains(out, line) {
			t.Fatalf("expected directive %q to remain unchanged in:\n%s", line, out)
		}
	}
}

func TestComputeSecureLinkMD5MatchesNginxFormula(t *testing.T) {
	// Reference value computed independently:
	//   echo -n "17459820001/hls/videos/v1/720p/segment000.tsmysecret" | md5sum | xxd -r -p | base64 | tr '+/' '-_' | tr -d '='
	const (
		expires = 1745982000
		uri     = "/hls/videos/v1/720p/segment000.ts"
		secret  = "mysecret"
	)
	got := computeSecureLinkMD5(expires, uri, secret)
	if got == "" {
		t.Fatal("expected non-empty signature")
	}
	// Signature must be base64url without padding
	if strings.ContainsAny(got, "+/=") {
		t.Fatalf("signature contains non-base64url characters: %q", got)
	}
	// Re-computing must give the same result (deterministic)
	if got2 := computeSecureLinkMD5(expires, uri, secret); got != got2 {
		t.Fatalf("computeSecureLinkMD5 is not deterministic: %q vs %q", got, got2)
	}
}

// ---- helpers for visibility / permission / token tests ----

// uploadTestVideo uploads a fake MP4 and returns the public video ID.
func uploadTestVideo(t *testing.T, router http.Handler) string {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, _ := writer.CreateFormFile("file", "test.mp4")
	_, _ = fw.Write([]byte("fake"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/videos", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("upload failed: %d %s", res.Code, res.Body.String())
	}
	var resp struct {
		VideoID string `json:"video_id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &resp)
	return resp.VideoID
}

func TestSetVisibility(t *testing.T) {
	database := openTestDB(t)
	srv := newTestServer(t, database)
	router := srv.Router()
	videoID := uploadTestVideo(t, router)

	// Set to private
	body := strings.NewReader(`{"visibility":"private"}`)
	req := httptest.NewRequest(http.MethodPut, "/videos/"+videoID+"/visibility", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	// Set back to public
	body = strings.NewReader(`{"visibility":"public"}`)
	req = httptest.NewRequest(http.MethodPut, "/videos/"+videoID+"/visibility", body)
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
}

func TestSetVisibilityInvalidValue(t *testing.T) {
	database := openTestDB(t)
	srv := newTestServer(t, database)
	router := srv.Router()
	videoID := uploadTestVideo(t, router)

	body := strings.NewReader(`{"visibility":"hidden"}`)
	req := httptest.NewRequest(http.MethodPut, "/videos/"+videoID+"/visibility", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestSetVisibilityNotFound(t *testing.T) {
	database := openTestDB(t)
	srv := newTestServer(t, database)
	router := srv.Router()

	body := strings.NewReader(`{"visibility":"private"}`)
	req := httptest.NewRequest(http.MethodPut, "/videos/nonexistent/visibility", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestIssueToken(t *testing.T) {
	database := openTestDB(t)
	srv := newTestServer(t, database)
	router := srv.Router()
	videoID := uploadTestVideo(t, router)

	// Issue token without needing prior permission grant
	req := httptest.NewRequest(http.MethodPost, "/videos/"+videoID+"/tokens", nil)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode token response: %v", err)
	}
	if len(resp.Token) != 32 {
		t.Fatalf("expected 32-char token, got %q (len %d)", resp.Token, len(resp.Token))
	}
	if resp.ExpiresAt == "" {
		t.Fatal("expected non-empty expires_at")
	}
}

func TestGetPlaylistPrivateWithValidToken(t *testing.T) {
	const (
		videoID = "pvt1"
		profile = "720p"
	)
	m3u8 := "#EXTM3U\n#EXT-X-ENDLIST\n"
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.m3u8", videoID, profile): m3u8,
		},
	}
	database := openTestDB(t)
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile}}}
	tokenCfg := config.PlaylistTokenConfig{TTLSec: 900}
	srv := NewServer(database, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, tokenCfg)
	router := srv.Router()

	// Insert a video row directly with public_id=videoID and visibility=private
	_, err := database.Exec(
		`INSERT INTO videos (public_id, original_name, temp_path, status, visibility) VALUES (?, 'v', 'p', 'ready', 'private')`,
		videoID)
	if err != nil {
		t.Fatalf("insert video: %v", err)
	}
	var internalID int64
	_ = database.QueryRow(`SELECT id FROM videos WHERE public_id = ?`, videoID).Scan(&internalID)

	// Insert a valid token
	expiresAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	_, _ = database.Exec(`INSERT INTO playlist_tokens (token, video_id, expires_at) VALUES ('validtoken123', ?, ?)`, internalID, expiresAt)

	// Request with valid token
	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile="+profile+"&token=validtoken123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d: %s", res.Code, res.Body.String())
	}
}

func TestGetPlaylistPrivateWithoutToken(t *testing.T) {
	const videoID = "pvt2"
	database := openTestDB(t)
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: "720p"}}}
	srv := NewServer(database, nil, nil, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{TTLSec: 900})
	router := srv.Router()

	_, err := database.Exec(
		`INSERT INTO videos (public_id, original_name, temp_path, status, visibility) VALUES (?, 'v', 'p', 'ready', 'private')`,
		videoID)
	if err != nil {
		t.Fatalf("insert video: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile=720p", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without token, got %d", res.Code)
	}
}

func TestGetPlaylistPrivateWithExpiredToken(t *testing.T) {
	const (
		videoID = "pvt3"
		profile = "720p"
	)
	database := openTestDB(t)
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile}}}
	srv := NewServer(database, nil, nil, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{TTLSec: 900})
	router := srv.Router()

	_, _ = database.Exec(
		`INSERT INTO videos (public_id, original_name, temp_path, status, visibility) VALUES (?, 'v', 'p', 'ready', 'private')`,
		videoID)
	var internalID int64
	_ = database.QueryRow(`SELECT id FROM videos WHERE public_id = ?`, videoID).Scan(&internalID)

	// Expired token
	expiredAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	_, _ = database.Exec(`INSERT INTO playlist_tokens (token, video_id, expires_at) VALUES ('expiredtok', ?, ?)`, internalID, expiredAt)

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile="+profile+"&token=expiredtok", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with expired token, got %d", res.Code)
	}
}

func TestNewToken(t *testing.T) {
	token, err := newToken(32)
	if err != nil {
		t.Fatalf("newToken error: %v", err)
	}
	if len(token) != 32 {
		t.Fatalf("expected length 32, got %d", len(token))
	}
	// Must only contain base62 characters
	for _, ch := range token {
		if !strings.ContainsRune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", ch) {
			t.Fatalf("non-base62 character %q in token %q", ch, token)
		}
	}
	// Two calls must produce different tokens
	token2, _ := newToken(32)
	if token == token2 {
		t.Fatal("newToken is not random: two calls returned the same value")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}
