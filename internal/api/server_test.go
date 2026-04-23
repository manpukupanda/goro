package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"goro/internal/db"
	"goro/internal/queue"
)

func TestUploadVideoCreatesVideoAndJob(t *testing.T) {
	database := openTestDB(t)
	q := queue.New(database)
	router := NewServer(q).Router()

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
		VideoID int64 `json:"video_id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.VideoID <= 0 {
		t.Fatalf("expected positive video_id, got %d", response.VideoID)
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
WHERE v.id = ?
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
	q := queue.New(database)
	router := NewServer(q).Router()

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
