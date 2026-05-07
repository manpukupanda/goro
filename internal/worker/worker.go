package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
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

func Start(q *queue.Queue, s uploader, hlsConfig config.HLSConfig, thumbnailConfig config.ThumbnailConfig) {
	log.Println("Worker started")

	for {
		job := q.FetchPending()
		if job == nil {
			time.Sleep(1 * time.Second)
			continue
		}

		log.Printf("processing job %d for video %d", job.ID, job.VideoID)
		if err := processJob(context.Background(), q, s, job, hlsConfig, thumbnailConfig); err != nil {
			log.Printf("job %d failed: %v", job.ID, err)
			q.MarkFailed(job.ID, err)
		} else {
			q.MarkDone(job.ID)
		}
	}
}

func processJob(ctx context.Context, q *queue.Queue, s uploader, job *queue.Job, hlsConfig config.HLSConfig, thumbnailConfig config.ThumbnailConfig) error {
	workDir, err := os.MkdirTemp("", fmt.Sprintf("goro-hls-%d-", job.VideoID))
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	// Probe metadata from the original file. Failures are non-fatal: the job
	// continues and the metadata columns stay NULL until a later attempt.
	var duration float64
	meta, err := probeVideoMetadata(ctx, job.InputMP4)
	if err != nil {
		log.Printf("probe: failed to get metadata for %s: %v", job.PublicID, err)
	} else {
		duration = meta.DurationSec
		if err := q.UpdateVideoMetadata(job.PublicID, meta); err != nil {
			log.Printf("probe: failed to save metadata for %s: %v", job.PublicID, err)
		}
	}

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

	// Generate and upload thumbnails. Failures are non-fatal.
	generateAndUploadThumbnails(ctx, s, job.InputMP4, job.PublicID, workDir, thumbnailConfig, duration)

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

// ffprobeOutput is the top-level structure of ffprobe's JSON output.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat  `json:"format"`
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
	BitRate    string `json:"bit_rate"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
	BitRate  string `json:"bit_rate"`
}

// probeVideoMetadata runs ffprobe on inputPath and returns extracted metadata.
func probeVideoMetadata(ctx context.Context, inputPath string) (queue.VideoMetadata, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_streams",
		"-show_format",
		"-print_format", "json",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return queue.VideoMetadata{}, fmt.Errorf("ffprobe failed: %w", err)
	}
	return parseFFprobeOutput(out)
}

// parseFFprobeOutput parses raw ffprobe JSON output into VideoMetadata.
// It is kept separate from probeVideoMetadata to allow unit testing without
// running a real ffprobe binary.
func parseFFprobeOutput(data []byte) (queue.VideoMetadata, error) {
	var result ffprobeOutput
	if err := json.Unmarshal(data, &result); err != nil {
		return queue.VideoMetadata{}, fmt.Errorf("ffprobe json parse: %w", err)
	}

	var meta queue.VideoMetadata

	// Duration and total bitrate come from the format section.
	if result.Format.Duration != "" {
		meta.DurationSec, _ = strconv.ParseFloat(result.Format.Duration, 64)
	}
	if result.Format.BitRate != "" {
		meta.Bitrate, _ = strconv.ParseInt(result.Format.BitRate, 10, 64)
	}

	// Use the first video stream for resolution, codec, and framerate.
	for _, stream := range result.Streams {
		if stream.CodecType != "video" {
			continue
		}
		meta.Width = stream.Width
		meta.Height = stream.Height
		meta.VideoCodec = stream.CodecName
		// Prefer stream-level bitrate when available.
		if stream.BitRate != "" {
			if br, err := strconv.ParseInt(stream.BitRate, 10, 64); err == nil && br > 0 {
				meta.Bitrate = br
			}
		}
		// Store the rational framerate string; discard degenerate values.
		fr := stream.RFrameRate
		if fr != "" && fr != "0/0" && fr != "0/1" {
			meta.Framerate = fr
		}
		break
	}

	return meta, nil
}

// thumbnailFilterFrames returns the number of frames to analyse with the ffmpeg
// thumbnail filter so that short videos are fully scanned rather than only the
// first 100 frames (the ffmpeg default).
func thumbnailFilterFrames(duration float64) int {
	const defaultFrames = 100
	const assumedFPS = 30.0
	estimated := int(math.Ceil(duration * assumedFPS))
	if estimated > 0 && estimated < defaultFrames {
		return estimated
	}
	return defaultFrames
}

// fixedSecondTimestamp computes the seek position (in seconds) for a
// "fixed_second" thumbnail.  If specSec > 0 it is used directly (capped to
// the video duration).  Otherwise the auto-rule applies: videos that are at
// least 5 s long use the 5 s mark; shorter videos use duration/2.
func fixedSecondTimestamp(specSec, duration float64) float64 {
	if specSec > 0 {
		sec := specSec
		if sec >= duration && duration > 0 {
			sec = duration / 2
		}
		return sec
	}
	// Auto rule
	if duration >= 5 {
		return 5.0
	}
	return duration / 2
}

// generateThumbnail generates a single thumbnail JPEG and returns its file path.
func generateThumbnail(ctx context.Context, inputPath, workDir string, spec config.ThumbnailSpec, duration float64) (string, error) {
	outPath := filepath.Join(workDir, spec.Name+".jpg")

	var args []string
	switch spec.Type {
	case "fixed_second":
		sec := fixedSecondTimestamp(spec.DurationSec, duration)
		args = []string{
			"-y",
			"-ss", strconv.FormatFloat(sec, 'f', 3, 64),
			"-i", inputPath,
			"-vframes", "1",
			"-q:v", "2",
			outPath,
		}
	case "representative":
		n := thumbnailFilterFrames(duration)
		args = []string{
			"-y",
			"-i", inputPath,
			"-vf", fmt.Sprintf("thumbnail=n=%d", n),
			"-frames:v", "1",
			"-q:v", "2",
			outPath,
		}
	default:
		return "", fmt.Errorf("unknown thumbnail type: %s", spec.Type)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg thumbnail failed: %w: %s", err, string(output))
	}
	return outPath, nil
}

// generateAndUploadThumbnails generates thumbnails for all specs in thumbnailConfig
// and uploads them to storage. The caller provides the pre-computed video duration
// so that a second ffprobe invocation is avoided. Individual spec failures are
// logged but do not fail the overall job.
func generateAndUploadThumbnails(ctx context.Context, s uploader, inputPath, publicID, workDir string, thumbnailConfig config.ThumbnailConfig, duration float64) {
	if len(thumbnailConfig.Specs) == 0 {
		return
	}

	thumbDir := filepath.Join(workDir, "thumbnails")
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		log.Printf("thumbnail: failed to create work dir for %s: %v", publicID, err)
		return
	}

	for _, spec := range thumbnailConfig.Specs {
		outPath, err := generateThumbnail(ctx, inputPath, thumbDir, spec, duration)
		if err != nil {
			log.Printf("thumbnail: failed to generate %s for %s: %v", spec.Name, publicID, err)
			continue
		}
		objectName := fmt.Sprintf("videos/%s/thumbnails/%s.jpg", publicID, spec.Name)
		if err := s.UploadFile(ctx, objectName, outPath, "image/jpeg"); err != nil {
			log.Printf("thumbnail: failed to upload %s for %s: %v", spec.Name, publicID, err)
		}
	}
}
