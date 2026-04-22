package queue

import (
	"database/sql"
	"log"
)

type Queue struct {
	db *sql.DB
}

type Job struct {
	ID int64
}

func New(db *sql.DB) *Queue {
	return &Queue{db: db}
}

func (q *Queue) FetchPending() *Job {
	row := q.db.QueryRow(`SELECT id FROM jobs WHERE status='pending' LIMIT 1`)
	var id int64
	err := row.Scan(&id)
	if err != nil {
		return nil
	}
	return &Job{ID: id}
}

func (q *Queue) MarkDone(id int64) {
	_, err := q.db.Exec(`UPDATE jobs SET status='done' WHERE id=?`, id)
	if err != nil {
		log.Printf("failed to mark done: %v", err)
	}
}
