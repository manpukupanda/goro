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

	// 最小の jobs テーブル
	db.Exec(`
CREATE TABLE IF NOT EXISTS jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT,
    input TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`)

	return db, nil
}
