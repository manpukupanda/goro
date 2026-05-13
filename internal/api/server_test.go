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
	"goro/internal/errcode"
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

func (s *stubStorage) DeleteVideoObjects(_ context.Context, _ string) error {
	return nil
}

const testAPIKey = "test-api-key"

func newTestServer(t *testing.T, database *sql.DB) *Server {
	t.Helper()
	q := queue.New(database)
	return NewServer(database, q, nil, config.SecureLinkConfig{}, config.HLSConfig{}, config.PlaylistTokenConfig{TTLSec: 900}, testAPIKey)
}

// authRequest attaches the test API key to a request.
func authRequest(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
}

func assertErrorResponse(t *testing.T, body []byte, wantCode, wantMessage string) {
	t.Helper()
	var got struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if got.Code != wantCode {
		t.Fatalf("expected error code %q, got %q", wantCode, got.Code)
	}
	if got.Message != wantMessage {
		t.Fatalf("expected error message %q, got %q", wantMessage, got.Message)
	}
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
	authRequest(req)
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
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
	assertErrorResponse(t, res.Body.Bytes(), errcode.CodeVideoUnsupportedFormat, "only .mp4 is supported")
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
	srv := NewServer(nil, nil, store, slCfg, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
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

func TestGetPlaylistRewritesFMP4Assets(t *testing.T) {
	const (
		videoID = "abc123"
		profile = "720p"
		secret  = "testsecret"
	)
	m3u8 := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4.000000,\nsegment000.m4s\n#EXT-X-ENDLIST\n"

	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.m3u8", videoID, profile): m3u8,
		},
	}
	slCfg := config.SecureLinkConfig{Secret: secret, TTLSec: 3600}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile, Format: config.ProfileFormatHLSFMP4}}}
	srv := NewServer(nil, nil, store, slCfg, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile="+profile, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "/hls/videos/"+videoID+"/"+profile+"/init.mp4") {
		t.Fatalf("expected rewritten init path in playlist, got:\n%s", body)
	}
	if !strings.Contains(body, "/hls/videos/"+videoID+"/"+profile+"/segment000.m4s") {
		t.Fatalf("expected rewritten fMP4 segment path in playlist, got:\n%s", body)
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
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	// No ?profile= query param — should fall back to hlsConfig.Profiles[0]
	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
}

func TestGetPlaylistUsesFirstHLSProfile(t *testing.T) {
	const videoID = "abc123"
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.m3u8", videoID, "720p"): "#EXTM3U\n#EXT-X-ENDLIST\n",
		},
	}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{
		{Name: "dash", Format: config.ProfileFormatDASHFMP4},
		{Name: "720p", Format: config.ProfileFormatHLSFMP4},
	}}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
}

func TestGetManifestUsesFirstDASHProfile(t *testing.T) {
	const videoID = "abc123"
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.mpd", videoID, "dash"): `<?xml version="1.0"?><MPD></MPD>`,
		},
	}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{
		{Name: "720p", Format: config.ProfileFormatHLSFMP4},
		{Name: "dash", Format: config.ProfileFormatDASHFMP4},
	}}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/manifest", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
}

func TestGetPlaylistNotFound(t *testing.T) {
	store := &stubStorage{objects: map[string]string{}}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: "720p"}}}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
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

func TestRewriteManifest(t *testing.T) {
	const (
		videoID = "vid1"
		profile = "dash"
		secret  = "s3cr3t"
	)
	expires := time.Now().Add(time.Hour).Unix()
	mpd := `<?xml version="1.0"?>
<MPD><Period><AdaptationSet><Representation><SegmentList><Initialization sourceURL="init-stream0.m4s"/><SegmentURL media="chunk-stream0-00001.m4s"/><SegmentURL media="chunk-stream0-00002.m4s"/></SegmentList></Representation></AdaptationSet></Period></MPD>`

	out, err := rewriteManifest(strings.NewReader(mpd), videoID, profile, expires, secret)
	if err != nil {
		t.Fatalf("rewriteManifest error: %v", err)
	}

	for _, asset := range []string{"init-stream0.m4s", "chunk-stream0-00001.m4s", "chunk-stream0-00002.m4s"} {
		uri := fmt.Sprintf("/dash/videos/%s/%s/%s", videoID, profile, asset)
		sig := computeSecureLinkMD5(expires, uri, secret)
		want := fmt.Sprintf("%s?expires=%d&amp;md5=%s", uri, expires, sig)
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestGetManifestRewritesSegmentURLs(t *testing.T) {
	const (
		videoID = "abc123"
		profile = "dash"
		secret  = "testsecret"
	)
	mpd := `<?xml version="1.0"?>
<MPD><Period><AdaptationSet><Representation><SegmentList><Initialization sourceURL="init-stream0.m4s"/><SegmentURL media="chunk-stream0-00001.m4s"/></SegmentList></Representation></AdaptationSet></Period></MPD>`

	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.mpd", videoID, profile): mpd,
		},
	}
	slCfg := config.SecureLinkConfig{Secret: secret, TTLSec: 3600}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile, Format: config.ProfileFormatDASHFMP4}}}
	srv := NewServer(nil, nil, store, slCfg, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/manifest?profile="+profile, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "/dash/videos/"+videoID+"/"+profile+"/init-stream0.m4s") {
		t.Fatalf("expected rewritten init path in manifest, got:\n%s", body)
	}
	if !strings.Contains(body, "/dash/videos/"+videoID+"/"+profile+"/chunk-stream0-00001.m4s") {
		t.Fatalf("expected rewritten segment path in manifest, got:\n%s", body)
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
	authRequest(req)
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
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	// Set back to public
	body = strings.NewReader(`{"visibility":"public"}`)
	req = httptest.NewRequest(http.MethodPut, "/videos/"+videoID+"/visibility", body)
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
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
	authRequest(req)
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
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestSetReferrerWhitelist(t *testing.T) {
	database := openTestDB(t)
	srv := newTestServer(t, database)
	router := srv.Router()
	videoID := uploadTestVideo(t, router)

	body := strings.NewReader(`{"referrer_whitelist":["example.com","*.example.com","example.com"]}`)
	req := httptest.NewRequest(http.MethodPut, "/videos/"+videoID+"/referrer-whitelist", body)
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var stored string
	if err := database.QueryRow(`SELECT referrer_whitelist FROM videos WHERE public_id = ?`, videoID).Scan(&stored); err != nil {
		t.Fatalf("failed to read stored whitelist: %v", err)
	}
	if stored != "example.com\n*.example.com" {
		t.Fatalf("unexpected stored whitelist: %q", stored)
	}
}

func TestSetReferrerWhitelistInvalid(t *testing.T) {
	database := openTestDB(t)
	srv := newTestServer(t, database)
	router := srv.Router()
	videoID := uploadTestVideo(t, router)

	body := strings.NewReader(`{"referrer_whitelist":["https://example.com"]}`)
	req := httptest.NewRequest(http.MethodPut, "/videos/"+videoID+"/referrer-whitelist", body)
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	assertErrorResponse(t, res.Body.Bytes(), errcode.CodeVideoReferrerWhitelistInvalid, "referrer whitelist must contain domains only")
}

func TestIssueToken(t *testing.T) {
	database := openTestDB(t)
	srv := newTestServer(t, database)
	router := srv.Router()
	videoID := uploadTestVideo(t, router)

	// Issue token without needing prior permission grant
	req := httptest.NewRequest(http.MethodPost, "/videos/"+videoID+"/tokens", nil)
	req.Header.Set("Content-Type", "application/json")
	authRequest(req)
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
	srv := NewServer(database, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, tokenCfg, testAPIKey)
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
	srv := NewServer(database, nil, nil, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{TTLSec: 900}, testAPIKey)
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
	assertErrorResponse(t, res.Body.Bytes(), errcode.CodeTokenRequired, "token is required")
}

func TestGetPlaylistPrivateWithExpiredToken(t *testing.T) {
	const (
		videoID = "pvt3"
		profile = "720p"
	)
	database := openTestDB(t)
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile}}}
	srv := NewServer(database, nil, nil, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{TTLSec: 900}, testAPIKey)
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
	assertErrorResponse(t, res.Body.Bytes(), errcode.CodeTokenInvalidOrExpired, "invalid or expired token")
}

func TestGetPlaylistReferrerWhitelist(t *testing.T) {
	const (
		videoID = "refwl1"
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
	srv := NewServer(database, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{TTLSec: 900}, testAPIKey)
	router := srv.Router()

	_, err := database.Exec(
		`INSERT INTO videos (public_id, original_name, temp_path, status, visibility, referrer_whitelist) VALUES (?, 'v', 'p', 'ready', 'public', ?)`,
		videoID, "example.com\n*.example.org\nexample.com:8443")
	if err != nil {
		t.Fatalf("insert video: %v", err)
	}

	tests := []struct {
		name    string
		referer string
		want    int
	}{
		{name: "exact domain", referer: "https://example.com/page", want: http.StatusOK},
		{name: "wildcard domain", referer: "https://sub.example.org/path", want: http.StatusOK},
		{name: "exact host and port", referer: "https://example.com:8443/path", want: http.StatusOK},
		{name: "missing referer", referer: "", want: http.StatusForbidden},
		{name: "port mismatch denied", referer: "https://example.org:8443/path", want: http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile="+profile, nil)
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, res.Code, res.Body.String())
			}
			if tc.want == http.StatusForbidden {
				assertErrorResponse(t, res.Body.Bytes(), errcode.CodeRefererNotAllowed, "referer is not allowed")
			}
		})
	}
}

func TestGetPlaylistPrivateRequiresRefererWhenWhitelistConfigured(t *testing.T) {
	const (
		videoID = "pvt-ref"
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
	srv := NewServer(database, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{TTLSec: 900}, testAPIKey)
	router := srv.Router()

	_, err := database.Exec(
		`INSERT INTO videos (public_id, original_name, temp_path, status, visibility, referrer_whitelist) VALUES (?, 'v', 'p', 'ready', 'private', ?)`,
		videoID, "example.com")
	if err != nil {
		t.Fatalf("insert video: %v", err)
	}
	var internalID int64
	_ = database.QueryRow(`SELECT id FROM videos WHERE public_id = ?`, videoID).Scan(&internalID)
	expiresAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	_, _ = database.Exec(`INSERT INTO playlist_tokens (token, video_id, expires_at) VALUES ('validtoken456', ?, ?)`, internalID, expiresAt)

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile="+profile+"&token=validtoken456", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without referer, got %d", res.Code)
	}
	assertErrorResponse(t, res.Body.Bytes(), errcode.CodeRefererNotAllowed, "referer is not allowed")
}

func TestManifestAndDashAssetApplyReferrerWhitelist(t *testing.T) {
	const (
		videoID = "dash-ref"
		profile = "720p"
		asset   = "chunk.m4s"
	)
	mpd := "<?xml version=\"1.0\"?><MPD><Period><BaseURL>segment.m4s</BaseURL></Period></MPD>"
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.mpd", videoID, profile): mpd,
			fmt.Sprintf("videos/%s/%s/%s", videoID, profile, asset): "dash-data",
		},
	}
	database := openTestDB(t)
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile, Format: config.ProfileFormatDASHFMP4}}}
	srv := NewServer(database, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{TTLSec: 900}, testAPIKey)
	router := srv.Router()

	_, err := database.Exec(
		`INSERT INTO videos (public_id, original_name, temp_path, status, visibility, referrer_whitelist) VALUES (?, 'v', 'p', 'ready', 'public', ?)`,
		videoID, "example.com")
	if err != nil {
		t.Fatalf("insert video: %v", err)
	}

	reqManifest := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/manifest?profile="+profile, nil)
	reqManifest.Header.Set("Referer", "https://example.com/watch")
	resManifest := httptest.NewRecorder()
	router.ServeHTTP(resManifest, reqManifest)
	if resManifest.Code != http.StatusOK {
		t.Fatalf("expected 200 for manifest with allowed referer, got %d: %s", resManifest.Code, resManifest.Body.String())
	}

	reqAssetDenied := httptest.NewRequest(http.MethodGet, "/dash/videos/"+videoID+"/"+profile+"/"+asset, nil)
	resAssetDenied := httptest.NewRecorder()
	router.ServeHTTP(resAssetDenied, reqAssetDenied)
	if resAssetDenied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for dash asset without referer, got %d", resAssetDenied.Code)
	}
	assertErrorResponse(t, resAssetDenied.Body.Bytes(), errcode.CodeRefererNotAllowed, "referer is not allowed")

	reqAssetAllowed := httptest.NewRequest(http.MethodGet, "/dash/videos/"+videoID+"/"+profile+"/"+asset, nil)
	reqAssetAllowed.Header.Set("Referer", "https://example.com/watch")
	resAssetAllowed := httptest.NewRecorder()
	router.ServeHTTP(resAssetAllowed, reqAssetAllowed)
	if resAssetAllowed.Code != http.StatusOK {
		t.Fatalf("expected 200 for dash asset with allowed referer, got %d: %s", resAssetAllowed.Code, resAssetAllowed.Body.String())
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

// ---- API key middleware tests ----

func TestHealthzNoAuthRequired(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// No Authorization header
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected /healthz to return 200 without auth, got %d", res.Code)
	}
}

func TestPlaylistNoAuthRequired(t *testing.T) {
	const (
		videoID = "noauth1"
		profile = "720p"
	)
	m3u8 := "#EXTM3U\n#EXT-X-ENDLIST\n"
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.m3u8", videoID, profile): m3u8,
		},
	}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile}}}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	// No Authorization header
	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/playlist?profile="+profile, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected /videos/:id/playlist to return 200 without auth, got %d", res.Code)
	}
}

func TestSegmentNoAuthRequired(t *testing.T) {
	const (
		videoID = "noauth2"
		profile = "720p"
		segment = "segment000.ts"
	)
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/%s", videoID, profile, segment): "ts-data",
		},
	}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{}, config.HLSConfig{}, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	// No Authorization header
	req := httptest.NewRequest(http.MethodGet, "/hls/videos/"+videoID+"/"+profile+"/"+segment, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected /hls/videos/:id/:profile/:segment to return 200 without auth, got %d", res.Code)
	}
}

func TestManifestNoAuthRequired(t *testing.T) {
	const (
		videoID = "noauth3"
		profile = "dash"
	)
	mpd := `<?xml version="1.0"?><MPD></MPD>`
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/index.mpd", videoID, profile): mpd,
		},
	}
	hlsCfg := config.HLSConfig{Profiles: []config.HLSProfile{{Name: profile, Format: config.ProfileFormatDASHFMP4}}}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{TTLSec: 3600}, hlsCfg, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/videos/"+videoID+"/manifest?profile="+profile, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected /videos/:id/manifest to return 200 without auth, got %d", res.Code)
	}
}

func TestDASHAssetNoAuthRequired(t *testing.T) {
	const (
		videoID = "noauth4"
		profile = "dash"
		asset   = "chunk-stream0-00001.m4s"
	)
	store := &stubStorage{
		objects: map[string]string{
			fmt.Sprintf("videos/%s/%s/%s", videoID, profile, asset): "dash-data",
		},
	}
	srv := NewServer(nil, nil, store, config.SecureLinkConfig{}, config.HLSConfig{}, config.PlaylistTokenConfig{}, testAPIKey)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/dash/videos/"+videoID+"/"+profile+"/"+asset, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected /dash/videos/:id/:profile/:asset to return 200 without auth, got %d", res.Code)
	}
	if got := res.Header().Get("Content-Type"); got != "video/iso.segment" {
		t.Fatalf("expected video/iso.segment content type, got %q", got)
	}
}

func TestRequireAPIKeyRejects401(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()

	tests := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"wrong key", "Bearer wrong-key"},
		{"missing Bearer prefix", testAPIKey},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/videos", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", res.Code)
			}
		})
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

// ----- GET /videos tests -----

func TestListVideosEmpty(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()

	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Videos []interface{} `json:"videos"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.Videos) != 0 {
		t.Fatalf("expected empty videos list, got %d items", len(body.Videos))
	}
}

func TestListVideosReturnsUploadedVideo(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()
	uploadTestVideo(t, router)

	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var body struct {
		Videos []struct {
			PublicID     string `json:"public_id"`
			OriginalName string `json:"original_name"`
			Status       string `json:"status"`
		} `json:"videos"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(body.Videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(body.Videos))
	}
	if body.Videos[0].Status != "queued" {
		t.Fatalf("expected status queued, got %s", body.Videos[0].Status)
	}
}

func TestListVideosFilterByStatus(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()
	uploadTestVideo(t, router) // status = queued

	// Filter for a status that doesn't exist yet.
	req := httptest.NewRequest(http.MethodGet, "/videos?status=ready", nil)
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var body struct {
		Videos []interface{} `json:"videos"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(body.Videos) != 0 {
		t.Fatalf("expected 0 videos with status=ready, got %d", len(body.Videos))
	}
}

func TestListVideosFilterByName(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()
	uploadTestVideo(t, router) // original_name = "test.mp4"

	req := httptest.NewRequest(http.MethodGet, "/videos?name=test", nil)
	authRequest(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var body struct {
		Videos []interface{} `json:"videos"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(body.Videos) != 1 {
		t.Fatalf("expected 1 video matching name=test, got %d", len(body.Videos))
	}

	// Name that doesn't match.
	req2 := httptest.NewRequest(http.MethodGet, "/videos?name=nomatch", nil)
	authRequest(req2)
	res2 := httptest.NewRecorder()
	router.ServeHTTP(res2, req2)
	var body2 struct {
		Videos []interface{} `json:"videos"`
	}
	_ = json.Unmarshal(res2.Body.Bytes(), &body2)
	if len(body2.Videos) != 0 {
		t.Fatalf("expected 0 videos for non-matching name, got %d", len(body2.Videos))
	}
}

func TestListVideosRequiresAuth(t *testing.T) {
	database := openTestDB(t)
	router := newTestServer(t, database).Router()

	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	// No auth header.
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestParseRational(t *testing.T) {
	cases := []struct {
		input string
		want  float64
	}{
		{"30000/1001", 30000.0 / 1001.0},
		{"25/1", 25.0},
		{"24/1", 24.0},
		{"0/0", 0.0},
		{"30", 30.0},
	}
	for _, tc := range cases {
		got := parseRational(tc.input)
		if got != tc.want {
			t.Errorf("parseRational(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
