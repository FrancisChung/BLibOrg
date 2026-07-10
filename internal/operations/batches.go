package operations

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BatchSummary describes one batch as it appears in the log, for a caller
// that wants to list known batch IDs (e.g. to let a user pick which one to
// undo/redo) without needing to know them in advance.
type BatchSummary struct {
	BatchID     string
	Timestamp   time.Time
	EntryCount  int
	UndoneCount int
}

// ListBatches groups the log's entries by BatchID, in first-appearance
// (i.e. chronological, since the log is append-only) order.
func (l *Log) ListBatches() ([]BatchSummary, error) {
	entries, err := l.ReadAll()
	if err != nil {
		return nil, err
	}

	var summaries []BatchSummary
	index := map[string]int{}
	for _, e := range entries {
		i, ok := index[e.BatchID]
		if !ok {
			i = len(summaries)
			index[e.BatchID] = i
			summaries = append(summaries, BatchSummary{BatchID: e.BatchID, Timestamp: e.Timestamp})
		}
		summaries[i].EntryCount++
		if e.Undone {
			summaries[i].UndoneCount++
		}
	}
	return summaries, nil
}

// NextBatchID returns the next unused "YYYYMMDD-N" batch ID for today's
// date, derived entirely from existing batch IDs already in the log -- no
// separate counter file, so there's nothing that can drift out of sync with
// the log itself. Batch IDs from other dates, or not in this format at all
// (e.g. a hand-picked ID from before this generator existed), are ignored.
func NextBatchID(log *Log) (string, error) {
	summaries, err := log.ListBatches()
	if err != nil {
		return "", err
	}

	datePrefix := nowFunc().Format("20060102")
	max := 0
	for _, s := range summaries {
		rest, ok := strings.CutPrefix(s.BatchID, datePrefix+"-")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(rest); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%s-%d", datePrefix, max+1), nil
}
