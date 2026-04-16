package storage

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSQLiteStore_ConcurrentWritersAndReaders exercises the WAL +
// multi-connection configuration that replaced v0.1's
// MaxOpenConns=1. It runs writer goroutines alongside reader
// goroutines against the same store and asserts that:
//
//  1. No call deadlocks or times out.
//  2. Every write that reports success is eventually readable.
//  3. Readers interleave with writers — not just "all writes then all
//     reads" — by checking that Count() grows monotonically during
//     the test rather than only after it.
//
// This is the regression guard against the class of bugs we saw in
// the tenancy store (Rows iterator held across a second query under
// MaxOpenConns=1). The interaction log store does not have that
// pattern, but if a future refactor introduces it, this test will
// time out or deadlock.
func TestSQLiteStore_ConcurrentWritersAndReaders(t *testing.T) {
	store := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const (
		writers           = 4
		readers           = 4
		writesPerWriter   = 50
		totalExpectedRows = writers * writesPerWriter
	)

	var wg sync.WaitGroup
	var writesOK atomic.Int64
	var readsOK atomic.Int64
	var interleaveObserved atomic.Bool

	// Writers: each writer appends a fixed number of rows as fast as
	// it can. If the store were serialized on a single connection,
	// the workers would still finish — the test would fall back to
	// asserting correctness only, not concurrency. The
	// interleave-observed flag below is how we detect that readers
	// actually run while writers are still going.
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				if err := store.Log(ctx, sampleLog("concurrent-agent", "sess-")); err != nil {
					t.Errorf("writer %d: Log #%d failed: %v", id, i, err)
					return
				}
				writesOK.Add(1)
			}
		}(w)
	}

	// Readers: poll Count() and Query() while writers are running.
	// Mark interleaveObserved the first time we see a count that is
	// strictly less than the final expected total — proof that a
	// read completed while writes were still in flight.
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				n, err := store.Count(ctx)
				if err != nil {
					t.Errorf("reader %d: Count failed: %v", id, err)
					return
				}
				if n > 0 && n < int64(totalExpectedRows) {
					interleaveObserved.Store(true)
				}
				if _, err := store.Query(ctx, InteractionFilter{Limit: 10}); err != nil {
					t.Errorf("reader %d: Query failed: %v", id, err)
					return
				}
				readsOK.Add(1)
				if n >= int64(totalExpectedRows) {
					return
				}
				// Brief yield so the reader doesn't hot-loop the CPU.
				time.Sleep(time.Millisecond)
			}
		}(r)
	}

	wg.Wait()

	if writesOK.Load() != int64(totalExpectedRows) {
		t.Errorf("writesOK = %d, want %d", writesOK.Load(), totalExpectedRows)
	}
	if readsOK.Load() == 0 {
		t.Error("no successful reads")
	}
	if !interleaveObserved.Load() {
		t.Error("no interleave observed: reads never saw an in-progress write count. " +
			"This suggests MaxOpenConns may have been lowered back to 1 or WAL mode disabled.")
	}

	// Final assertion: every write is readable afterwards.
	n, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("final Count: %v", err)
	}
	if n != int64(totalExpectedRows) {
		t.Errorf("final count = %d, want %d", n, totalExpectedRows)
	}
}

// TestSQLiteStore_PragmasApplied verifies the actual SQLite pragmas
// that NewSQLiteStore configures via DSN. A typo in the DSN would
// silently fall back to default journal_mode=delete, which is the
// whole reason we bumped MaxOpenConns in the first place. Catching
// that via a direct PRAGMA query is the cheapest regression guard.
func TestSQLiteStore_PragmasApplied(t *testing.T) {
	store := testStore(t)

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var synchronous int
	if err := store.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	// SQLite returns synchronous as an integer: 0=OFF, 1=NORMAL, 2=FULL, 3=EXTRA.
	if synchronous != 1 {
		t.Errorf("synchronous = %d, want 1 (NORMAL)", synchronous)
	}

	if got := store.db.Stats().MaxOpenConnections; got != maxOpenConns {
		t.Errorf("MaxOpenConnections = %d, want %d", got, maxOpenConns)
	}
}
