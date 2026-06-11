package store

import (
	"database/sql"
	_ "modernc.org/sqlite"
)

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS vms (
			id           TEXT PRIMARY KEY,
			name         TEXT NOT NULL UNIQUE,
			vcpu         INTEGER NOT NULL,
			memory_gb    INTEGER NOT NULL,
			disk_gb      INTEGER NOT NULL,
			network_type TEXT NOT NULL,
			mac          TEXT NOT NULL,
			vnc_port     INTEGER NOT NULL,
			ip           TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL,
			created_at   TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS tasks (
			id         TEXT PRIMARY KEY,
			type       TEXT NOT NULL,
			ref_id     TEXT NOT NULL,
			status     TEXT NOT NULL,
			progress   INTEGER NOT NULL DEFAULT 0,
			message    TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
	`)
	return err
}
