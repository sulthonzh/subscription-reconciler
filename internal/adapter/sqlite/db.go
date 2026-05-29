package sqlite

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

var driverName = "sqlite"

func Open(dbPath string) (*sql.DB, error) {
	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		return nil, err
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA strict = ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, err
		}
	}

	return db, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func scanTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil
	}
	return &t
}
