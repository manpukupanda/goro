package worker

import (
	"log"
	"time"

	"goro/internal/queue"
	"goro/internal/storage"
)

func Start(q *queue.Queue, s *storage.S3) {
	log.Println("Worker started")

	for {
		job := q.FetchPending()
		if job != nil {
			log.Printf("Processing job: %d", job.ID)
			// TODO: HLS 変換処理
			q.MarkDone(job.ID)
		}

		time.Sleep(1 * time.Second)
	}
}
