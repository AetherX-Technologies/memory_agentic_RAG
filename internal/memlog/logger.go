// Package memlog provides structured JSON logging for the memory system.
// Uses Go's log/slog for structured output.
package memlog

import (
	"log/slog"
	"os"
)

// Logger is the shared structured logger for memory operations.
var Logger *slog.Logger

func init() {
	Logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// SetLevel changes the log level.
func SetLevel(level slog.Level) {
	Logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
}

// LogStore logs a memory store event.
func LogStore(id, memType, action string, source string) {
	Logger.Info("memory.store",
		"id", id,
		"type", memType,
		"action", action,
		"source", source,
	)
}

// LogRecall logs a memory recall event.
func LogRecall(query string, candidates, returned int) {
	Logger.Info("memory.recall",
		"query", query,
		"candidates", candidates,
		"returned", returned,
	)
}

// LogCleanup logs a cleanup cycle result.
func LogCleanup(expired, softDeleted, purged int) {
	Logger.Info("memory.cleanup",
		"expired", expired,
		"soft_deleted", softDeleted,
		"purged", purged,
	)
}

// LogDedup logs a dedup decision.
func LogDedup(action, reason, oldID, newID string) {
	Logger.Info("memory.dedup",
		"action", action,
		"reason", reason,
		"old_id", oldID,
		"new_id", newID,
	)
}
