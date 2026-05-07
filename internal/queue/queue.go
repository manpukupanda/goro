package queue

import (
	"crypto/rand"
	"database/sql"
	"log"
)

const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newPublicID returns a cryptographically random Base62 string of the given length.
func newPublicID(length int) (string, error) {
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

type Queue struct {
	db *sql.DB
}

type Job struct {
	ID       int64
	VideoID  int64
	PublicID string
	InputMP4 string
}

func New(db *sql.DB) *Queue {
	return &Queue{db: db}
}

func (q *Queue) EnqueueVideo(originalName, inputPath string) (string, error) {
	publicID, err := newPublicID(11)
	if err != nil {
		return "", err
	}

	tx, err := q.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO videos (public_id, original_name, temp_path, status) VALUES (?, ?, ?, 'queued')`, publicID, originalName, inputPath)
	if err != nil {
		return "", err
	}
	videoID, err := res.LastInsertId()
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(`INSERT INTO jobs (video_id, status, input) VALUES (?, 'pending', ?)`, videoID, inputPath); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return publicID, nil
}

func (q *Queue) FetchPending() *Job {
	tx, err := q.db.Begin()
	if err != nil {
		log.Printf("failed to begin transaction: %v", err)
		return nil
	}
	defer tx.Rollback()

	var job Job
	err = tx.QueryRow(`
SELECT j.id, j.video_id, v.public_id, j.input
FROM jobs j
JOIN videos v ON v.id = j.video_id
WHERE j.status='pending'
ORDER BY j.id
LIMIT 1
`).Scan(&job.ID, &job.VideoID, &job.PublicID, &job.InputMP4)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		log.Printf("failed to fetch job: %v", err)
		return nil
	}

	if _, err := tx.Exec(`UPDATE jobs SET status='processing', updated_at=CURRENT_TIMESTAMP WHERE id=?`, job.ID); err != nil {
		log.Printf("failed to lock job %d: %v", job.ID, err)
		return nil
	}

	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit job lock: %v", err)
		return nil
	}

	return &job
}

func (q *Queue) MarkDone(id int64) {
	tx, err := q.db.Begin()
	if err != nil {
		log.Printf("failed to begin done transaction: %v", err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE jobs SET status='done', updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		log.Printf("failed to mark done: %v", err)
		return
	}
	if _, err := tx.Exec(`UPDATE videos SET status='ready' WHERE id=(SELECT video_id FROM jobs WHERE id=?)`, id); err != nil {
		log.Printf("failed to mark video ready: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit done transaction: %v", err)
	}
}

// VideoMetadata holds extracted technical properties of an uploaded video.
type VideoMetadata struct {
	DurationSec     float64
	Width           int
	Height          int
	VideoCodec      string
	Bitrate         int64  // bits per second (format-level)
	Framerate       string // rational string e.g. "30000/1001"; empty if unknown
	ContainerFormat string // e.g. "mov", "matroska"
	AudioCodec      string
	AudioBitrate    int64 // bits per second (audio stream)
	SampleRate      int   // Hz
	Channels        int   // number of audio channels
	FileSize        int64 // bytes
	AspectRatio     string // e.g. "16:9"
	Rotation        int   // degrees, e.g. 90
	HasAudio        bool
	HasVideo        bool
}

// UpdateVideoMetadata stores extracted metadata on a video row.
func (q *Queue) UpdateVideoMetadata(publicID string, meta VideoMetadata) error {
	var framerate interface{}
	if meta.Framerate != "" {
		framerate = meta.Framerate
	}
	var containerFormat interface{}
	if meta.ContainerFormat != "" {
		containerFormat = meta.ContainerFormat
	}
	var audioCodec interface{}
	if meta.AudioCodec != "" {
		audioCodec = meta.AudioCodec
	}
	var audioBitrate interface{}
	if meta.AudioBitrate > 0 {
		audioBitrate = meta.AudioBitrate
	}
	var sampleRate interface{}
	if meta.SampleRate > 0 {
		sampleRate = meta.SampleRate
	}
	var channels interface{}
	if meta.Channels > 0 {
		channels = meta.Channels
	}
	var fileSize interface{}
	if meta.FileSize > 0 {
		fileSize = meta.FileSize
	}
	var aspectRatio interface{}
	if meta.AspectRatio != "" {
		aspectRatio = meta.AspectRatio
	}
	hasAudio := 0
	if meta.HasAudio {
		hasAudio = 1
	}
	hasVideo := 0
	if meta.HasVideo {
		hasVideo = 1
	}
	_, err := q.db.Exec(`
		UPDATE videos
		SET duration_sec     = ?,
		    width            = ?,
		    height           = ?,
		    video_codec      = ?,
		    bitrate          = ?,
		    framerate        = ?,
		    container_format = ?,
		    audio_codec      = ?,
		    audio_bitrate    = ?,
		    sample_rate      = ?,
		    channels         = ?,
		    file_size        = ?,
		    aspect_ratio     = ?,
		    rotation         = ?,
		    has_audio        = ?,
		    has_video        = ?
		WHERE public_id      = ?`,
		meta.DurationSec, meta.Width, meta.Height, meta.VideoCodec, meta.Bitrate, framerate,
		containerFormat, audioCodec, audioBitrate, sampleRate, channels, fileSize, aspectRatio, meta.Rotation,
		hasAudio, hasVideo, publicID)
	return err
}

func (q *Queue) MarkFailed(id int64, failureErr error) {
	tx, err := q.db.Begin()
	if err != nil {
		log.Printf("failed to begin failed transaction: %v", err)
		return
	}
	defer tx.Rollback()

	message := ""
	if failureErr != nil {
		message = failureErr.Error()
	}

	if _, err := tx.Exec(`UPDATE jobs SET status='failed', error_message=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, message, id); err != nil {
		log.Printf("failed to mark failed: %v", err)
		return
	}
	if _, err := tx.Exec(`UPDATE videos SET status='failed' WHERE id=(SELECT video_id FROM jobs WHERE id=?)`, id); err != nil {
		log.Printf("failed to mark video failed: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit failed transaction: %v", err)
	}
}
