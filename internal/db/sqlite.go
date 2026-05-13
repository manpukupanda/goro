package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS videos (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    public_id     TEXT    UNIQUE NOT NULL,
    original_name TEXT    NOT NULL,
    temp_path     TEXT    NOT NULL,
    status        TEXT    NOT NULL,
    visibility    TEXT    NOT NULL DEFAULT 'private',
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    duration_sec     REAL,
    width            INTEGER,
    height           INTEGER,
    video_codec      TEXT,
    bitrate          INTEGER,
    framerate        TEXT,
    container_format TEXT,
    audio_codec      TEXT,
    audio_bitrate    INTEGER,
    sample_rate      INTEGER,
    channels         INTEGER,
    file_size        INTEGER,
    aspect_ratio     TEXT,
    rotation         INTEGER,
    has_audio        INTEGER,
    has_video        INTEGER
);
`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS playlist_tokens (
    token      TEXT PRIMARY KEY,
    video_id   INTEGER NOT NULL,
    expires_at DATETIME NOT NULL,
    FOREIGN KEY (video_id) REFERENCES videos(id)
);
`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    input TEXT NOT NULL,
    error_code TEXT,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(video_id) REFERENCES videos(id)
);
`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_status_created ON jobs(status, created_at)`); err != nil {
		return nil, err
	}

	return db, nil
}
