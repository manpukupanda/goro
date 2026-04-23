package queue

import (
	"database/sql"
	"log"
)

type Queue struct {
	db *sql.DB
}

type Job struct {
	ID       int64
	VideoID  int64
	InputMP4 string
}

func New(db *sql.DB) *Queue {
	return &Queue{db: db}
}

func (q *Queue) EnqueueVideo(originalName, inputPath string) (int64, error) {
	tx, err := q.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO videos (original_name, temp_path, status) VALUES (?, ?, 'queued')`, originalName, inputPath)
	if err != nil {
		return 0, err
	}
	videoID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	if _, err := tx.Exec(`INSERT INTO jobs (video_id, status, input) VALUES (?, 'pending', ?)`, videoID, inputPath); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return videoID, nil
}

func (q *Queue) FetchPending() *Job {
	tx, err := q.db.Begin()
	if err != nil {
		log.Printf("failed to begin transaction: %v", err)
		return nil
	}
	defer tx.Rollback()

	var job Job
	err = tx.QueryRow(`SELECT id, video_id, input FROM jobs WHERE status='pending' ORDER BY id LIMIT 1`).
		Scan(&job.ID, &job.VideoID, &job.InputMP4)
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
