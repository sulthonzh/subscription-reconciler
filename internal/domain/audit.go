package domain

import "time"

type AuditEntry struct {
	ID            int64
	UserID        string
	TriggerID     string
	Source        Source
	PreviousState string
	NextState     string
	CreatedAt     time.Time
}
