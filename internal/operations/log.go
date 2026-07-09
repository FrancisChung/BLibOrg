package operations

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// LogEntry is one persisted record of a move operation, enough to
// reconstruct and reverse/replay the operation after an app restart.
type LogEntry struct {
	BatchID   string    `json:"batch_id"`
	Timestamp time.Time `json:"timestamp"`
	OpType    OpType    `json:"op_type"`
	OldPath   string    `json:"old_path"`
	NewPath   string    `json:"new_path"`
	Undone    bool      `json:"undone"`
}

// Log is an append-only JSONL file recording every move operation ever
// executed, so undo/redo works purely by re-reading this file -- it survives
// an app restart because it holds no in-memory state of its own.
type Log struct {
	mu   sync.Mutex
	path string
}

func NewLog(path string) *Log {
	return &Log{path: path}
}

// Append writes entries to the end of the log file, creating it if needed.
func (l *Log) Append(entries []LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log %s for append: %w", l.path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode log entry: %w", err)
		}
	}
	return nil
}

// ReadAll returns every entry in the log, in append order. A missing log
// file is treated as an empty log, not an error, since a fresh install
// won't have one yet.
func (l *Log) ReadAll() ([]LogEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.readAllLocked()
}

func (l *Log) readAllLocked() ([]LogEntry, error) {
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", l.path, err)
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e LogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("parse log entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan log %s: %w", l.path, err)
	}
	return entries, nil
}

func (l *Log) rewriteAllLocked(entries []LogEntry) error {
	f, err := os.Create(l.path)
	if err != nil {
		return fmt.Errorf("rewrite log %s: %w", l.path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode log entry: %w", err)
		}
	}
	return nil
}

// SetBatchUndone flips the Undone flag on every entry with the given
// batchID and persists the change. It is a no-op (returns nil) if batchID
// is not present in the log, so callers don't need to special-case unknown
// or already-processed batches.
func (l *Log) SetBatchUndone(batchID string, undone bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entries, err := l.readAllLocked()
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].BatchID == batchID {
			entries[i].Undone = undone
		}
	}
	return l.rewriteAllLocked(entries)
}
