package worker

import (
	"context"
	"fmt"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"goro/internal/config"
	"goro/internal/queue"
)

type uploader interface {
	UploadFile(ctx context.Context, objectName, filePath, contentType string) error
}

func Start(q *queue.Queue, s uploader, hlsConfig config.HLSConfig) {
	log.Println("Worker started")

	for {
		job := q.FetchPending()
		if job == nil {
			time.Sleep(1 * time.Second)
			continue
		}

		log.Printf("processing job %d for video %d", job.ID, job.VideoID)
		if err := processJob(context.Background(), s, job, hlsConfig); err != nil {
			log.Printf("job %d failed: %v", job.ID, err)
			q.MarkFailed(job.ID, err)
		} else {
			q.MarkDone(job.ID)
		}
	}
}

func processJob(ctx context.Context, s uploader, job *queue.Job, hlsConfig config.HLSConfig) error {
	workDir, err := os.MkdirTemp("", fmt.Sprintf("goro-hls-%d-", job.VideoID))
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	for _, profile := range hlsConfig.Profiles {
		profileDir := filepath.Join(workDir, profile.Name)
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return err
		}

		if err := runFFmpeg(ctx, job.InputMP4, profile, profileDir); err != nil {
			return err
		}

		if err := uploadProfileOutputs(ctx, s, job.PublicID, profile.Name, profileDir); err != nil {
			return err
		}
	}

	// Upload the original MP4 so it can be downloaded later.
	objectName := fmt.Sprintf("videos/%s/original.mp4", job.PublicID)
	if err := s.UploadFile(ctx, objectName, job.InputMP4, "video/mp4"); err != nil {
		return fmt.Errorf("failed to upload original mp4: %w", err)
	}

	_ = os.RemoveAll(filepath.Dir(job.InputMP4))
	return nil
}

func runFFmpeg(ctx context.Context, inputPath string, profile config.HLSProfile, profileDir string) error {
	args := buildFFmpegArgs(inputPath, profile, profileDir)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, string(output))
	}
	return nil
}

func buildFFmpegArgs(inputPath string, profile config.HLSProfile, profileDir string) []string {
	playlistPath := filepath.Join(profileDir, "index.m3u8")
	segmentPath := filepath.Join(profileDir, "segment%03d.ts")

	return []string{
		"-y",
		"-i", inputPath,
		"-vf", fmt.Sprintf("scale=%d:%d", profile.Width, profile.Height),
		"-c:v", "libx264",
		"-b:v", profile.VideoBitrate,
		"-c:a", "aac",
		"-b:a", profile.AudioBitrate,
		"-hls_time", strconv.Itoa(profile.SegmentSeconds),
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", segmentPath,
		playlistPath,
	}
}

func uploadProfileOutputs(ctx context.Context, s uploader, publicID string, profileName, profileDir string) error {
	entries, err := os.ReadDir(profileDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		filePath := filepath.Join(profileDir, filename)
		objectPath := fmt.Sprintf("videos/%s/%s/%s", publicID, profileName, filename)

		ext := filepath.Ext(filename)
		contentType := mime.TypeByExtension(ext)

		switch ext {
		case ".m3u8":
			contentType = "application/vnd.apple.mpegurl"
		case ".ts":
			contentType = "video/mp2t"
		}

		if err := s.UploadFile(ctx, objectPath, filePath, contentType); err != nil {
			return err
		}
	}

	return nil
}
