package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ── Backend factory ──

func testStores(t *testing.T) map[string]StatsStore {
	t.Helper()
	stores := map[string]StatsStore{
		"memory": newMemoryStatsStore(),
		"sqlite": newTestSQLiteStore(t),
	}

	// Optional: run against real PG / MySQL if env vars are set
	if dsn := os.Getenv("TEST_PG_DSN"); dsn != "" {
		store, err := newSQLStatsStore("pgx", dsn)
		if err != nil {
			t.Skipf("postgres not available: %v", err)
		}
		stores["postgres"] = store
	}
	if dsn := os.Getenv("TEST_MYSQL_DSN"); dsn != "" {
		store, err := newSQLStatsStore("mysql", dsn)
		if err != nil {
			t.Skipf("mysql not available: %v", err)
		}
		stores["mysql"] = store
	}
	return stores
}

// concurrentStores returns stores suitable for concurrent access tests.
func concurrentStores(t *testing.T) map[string]StatsStore {
	t.Helper()
	stores := map[string]StatsStore{
		"memory": newMemoryStatsStore(),
	}
	if dsn := os.Getenv("TEST_PG_DSN"); dsn != "" {
		store, err := newSQLStatsStore("pgx", dsn)
		if err != nil {
			t.Skipf("postgres not available: %v", err)
		}
		stores["postgres"] = store
	}
	if dsn := os.Getenv("TEST_MYSQL_DSN"); dsn != "" {
		store, err := newSQLStatsStore("mysql", dsn)
		if err != nil {
			t.Skipf("mysql not available: %v", err)
		}
		stores["mysql"] = store
	}
	return stores
}

func newTestSQLiteStore(t *testing.T) *sqlStatsStore {
	t.Helper()
	dir := t.TempDir()
	store, err := newSQLStatsStore("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.db.Close() })
	return store
}

// ── Common test helpers ──

func assertView(t *testing.T, store StatsStore, targetType, targetID, deviceID string, wantViews int64) {
	t.Helper()
	s := store.IncrementView(targetType, targetID, deviceID)
	if s.Views != wantViews {
		t.Fatalf("[%s:%s dev=%s] IncrementView: views=%d, want=%d", targetType, targetID, deviceID, s.Views, wantViews)
	}
}

func assertReaction(t *testing.T, store StatsStore, targetType, targetID, deviceID, reaction string, active bool, wantLikes, wantDislikes int64) {
	t.Helper()
	s, err := store.IncrementReaction(targetType, targetID, deviceID, reaction, active)
	if err != nil {
		t.Fatalf("[%s:%s dev=%s] IncrementReaction(%s, active=%v): unexpected error: %v", targetType, targetID, deviceID, reaction, active, err)
	}
	if s.Likes != wantLikes || s.Dislikes != wantDislikes {
		t.Fatalf("[%s:%s dev=%s] IncrementReaction(%s, active=%v): likes=%d dislikes=%d, want likes=%d dislikes=%d",
			targetType, targetID, deviceID, reaction, active, s.Likes, s.Dislikes, wantLikes, wantDislikes)
	}
}

func assertSnapshot(t *testing.T, store StatsStore, targetType, targetID string, wantViews, wantLikes, wantDislikes int64) {
	t.Helper()
	s := store.Snapshot(targetType, targetID)
	if s.Views != wantViews || s.Likes != wantLikes || s.Dislikes != wantDislikes {
		t.Fatalf("[%s:%s] Snapshot: views=%d likes=%d dislikes=%d, want views=%d likes=%d dislikes=%d",
			targetType, targetID, s.Views, s.Likes, s.Dislikes, wantViews, wantLikes, wantDislikes)
	}
}

// ── 1. Basic view tracking ──

func TestStatsBasicViews(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			// First view from a device
			assertView(t, store, "album", "x", "d1", 1)
			// Same device again — no-op
			assertView(t, store, "album", "x", "d1", 1)
			// Different device
			assertView(t, store, "album", "x", "d2", 2)
			// Third device
			assertView(t, store, "album", "x", "d3", 3)
			// Snapshot
			assertSnapshot(t, store, "album", "x", 3, 0, 0)
		})
	}
}

// ── 2. Basic reaction tracking ──

func TestStatsBasicReactions(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			// Like from device 1
			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			// Same device same reaction — no-op
			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			// Switch to dislike
			assertReaction(t, store, "album", "x", "d1", "dislike", true, 0, 1)
			// Same again — no-op
			assertReaction(t, store, "album", "x", "d1", "dislike", true, 0, 1)
			// Remove reaction
			assertReaction(t, store, "album", "x", "d1", "dislike", false, 0, 0)
			// Remove again — no-op
			assertReaction(t, store, "album", "x", "d1", "dislike", false, 0, 0)
		})
	}
}

// ── 3. Snapshot non-existent target returns zeros ──

func TestStatsSnapshotEmpty(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			s := store.Snapshot("album", "nonexistent")
			if s.Views != 0 || s.Likes != 0 || s.Dislikes != 0 {
				t.Fatalf("expected zero stats for non-existent target, got %#v", s)
			}
		})
	}
}

// ── 4. Multiple targets are independent ──

func TestStatsMultipleTargets(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			assertView(t, store, "album", "a", "d1", 1)
			assertView(t, store, "album", "b", "d1", 1)
			assertView(t, store, "album", "a", "d2", 2)

			assertReaction(t, store, "album", "a", "d1", "like", true, 1, 0)
			assertReaction(t, store, "album", "b", "d1", "dislike", true, 0, 1)

			// Verify independence
			assertSnapshot(t, store, "album", "a", 2, 1, 0)
			assertSnapshot(t, store, "album", "b", 1, 0, 1)
		})
	}
}

// ── 5. Different target types don't leak ──

func TestStatsCrossTypeIsolation(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			assertView(t, store, "album", "x", "d1", 1)
			assertView(t, store, "image", "x", "d1", 1)

			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			assertReaction(t, store, "image", "x", "d1", "dislike", true, 0, 1)

			assertSnapshot(t, store, "album", "x", 1, 1, 0)
			assertSnapshot(t, store, "image", "x", 1, 0, 1)
		})
	}
}

// ── 6. Reaction cycling (many switches) ──

func TestStatsReactionCycle(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			// Cycle: like → remove → dislike → remove → like
			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			assertReaction(t, store, "album", "x", "d1", "like", false, 0, 0)
			assertReaction(t, store, "album", "x", "d1", "dislike", true, 0, 1)
			assertReaction(t, store, "album", "x", "d1", "dislike", false, 0, 0)
			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			assertSnapshot(t, store, "album", "x", 0, 1, 0)
		})
	}
}

// ── 7. Many devices ──

func TestStatsManyDevices(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			n := 100
			for i := 0; i < n; i++ {
				assertView(t, store, "album", "x", fmt.Sprintf("dev-%d", i), int64(i+1))
			}
			assertSnapshot(t, store, "album", "x", int64(n), 0, 0)

			// 50 likes from different devices
			for i := 0; i < 50; i++ {
				assertReaction(t, store, "album", "x", fmt.Sprintf("dev-%d", i), "like", true, int64(i+1), 0)
			}
			// 50 dislikes from different devices
			for i := 50; i < 100; i++ {
				assertReaction(t, store, "album", "x", fmt.Sprintf("dev-%d", i), "dislike", true, 50, int64(i-50+1))
			}
			assertSnapshot(t, store, "album", "x", int64(n), 50, 50)
		})
	}
}

// ── 8. Invalid reaction returns error ──

func TestStatsInvalidReaction(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			invalidReactions := []string{"wow", "love", "hate", "", "Likes", "Like", "DISLIKE"}
			for _, r := range invalidReactions {
				if _, err := store.IncrementReaction("album", "x", "d1", r, true); err == nil {
					t.Fatalf("expected error for invalid reaction %q", r)
				}
			}
		})
	}
}

// ── 9. Concurrent access (memory / PG / MySQL) ──

func TestStatsConcurrent(t *testing.T) {
	for name, store := range concurrentStores(t) {
		t.Run(name, func(t *testing.T) {
			goroutines := 50
			viewsPerGoroutine := 20

			var wg sync.WaitGroup
			wg.Add(goroutines)

			for g := 0; g < goroutines; g++ {
				go func(id int) {
					defer wg.Done()
					for v := 0; v < viewsPerGoroutine; v++ {
						dev := fmt.Sprintf("dev-%d-%d", id, v)
						store.IncrementView("album", "concurrent", dev)
					}
				}(g)
			}
			wg.Wait()

			s := store.Snapshot("album", "concurrent")
			wantViews := int64(goroutines * viewsPerGoroutine)
			if s.Views != wantViews {
				t.Fatalf("concurrent views: got %d, want %d", s.Views, wantViews)
			}

			// Concurrent reactions
			routines := 30
			wg.Add(routines)
			for g := 0; g < routines; g++ {
				go func(id int) {
					defer wg.Done()
					store.IncrementReaction("album", "concurrent", fmt.Sprintf("rdev-%d", id), "like", true)
				}(g)
			}
			wg.Wait()

			s = store.Snapshot("album", "concurrent")
			if s.Likes != int64(routines) {
				t.Fatalf("concurrent likes: got %d, want %d", s.Likes, routines)
			}
		})
	}
}

// ── 9b. SQLite sequential write stress (SQLite serializes all writes) ──

func TestSQLiteWriteStress(t *testing.T) {
	store := newTestSQLiteStore(t)

	// Many sequential writes — this is SQLite's sweet spot
	for i := 0; i < 200; i++ {
		assertView(t, store, "album", "stress", fmt.Sprintf("dev-%d", i), int64(i+1))
	}
	assertSnapshot(t, store, "album", "stress", 200, 0, 0)

	// Reaction cycling on a single device — stress-test UPSERT logic
	for cycle := 0; cycle < 50; cycle++ {
		assertReaction(t, store, "album", "stress", "cycle-dev", "like", true, 1, 0)
		assertReaction(t, store, "album", "stress", "cycle-dev", "dislike", true, 0, 1)
		assertReaction(t, store, "album", "stress", "cycle-dev", "dislike", false, 0, 0)
	}
	// After 50 cycles, should be back to zero reactions
	assertSnapshot(t, store, "album", "stress", 200, 0, 0)
}

// ── 10. Reactions don't affect views ──

func TestStatsReactionsDontAffectViews(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			assertView(t, store, "album", "x", "d1", 1)
			assertView(t, store, "album", "x", "d2", 2)

			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			assertReaction(t, store, "album", "x", "d1", "dislike", true, 0, 1)
			assertReaction(t, store, "album", "x", "d1", "dislike", false, 0, 0)

			// Views should be unchanged after reaction operations
			assertSnapshot(t, store, "album", "x", 2, 0, 0)
		})
	}
}

// ── 11. Counters don't go negative ──

func TestStatsNoNegativeCounters(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			// Remove a reaction that was never set
			assertReaction(t, store, "album", "x", "d1", "like", false, 0, 0)
			assertReaction(t, store, "album", "x", "d1", "dislike", false, 0, 0)

			// Like then remove twice
			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			assertReaction(t, store, "album", "x", "d1", "like", false, 0, 0)
			assertReaction(t, store, "album", "x", "d1", "like", false, 0, 0) // already removed

			// Counters must be >= 0
			s := store.Snapshot("album", "x")
			if s.Likes < 0 || s.Dislikes < 0 || s.Views < 0 {
				t.Fatalf("negative counters: %#v", s)
			}
		})
	}
}

// ── 12. SQLite persists data across connections ──

func TestSQLitePersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	// First session
	store1, err := newSQLStatsStore("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	assertView(t, store1, "album", "x", "d1", 1)
	assertView(t, store1, "album", "x", "d2", 2)
	assertReaction(t, store1, "album", "x", "d1", "like", true, 1, 0)
	assertReaction(t, store1, "album", "x", "d2", "dislike", true, 1, 1)
	store1.db.Close()

	// Second session — data must persist
	store2, err := newSQLStatsStore("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.db.Close()

	assertSnapshot(t, store2, "album", "x", 2, 1, 1)

	// Additional operations in second session
	assertView(t, store2, "album", "x", "d3", 3)
	assertReaction(t, store2, "album", "x", "d3", "like", true, 2, 1)
	assertSnapshot(t, store2, "album", "x", 3, 2, 1)
}

// ── 13. Active=false with no existing reaction is a no-op ──

func TestStatsRemoveNonexistentReaction(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			assertReaction(t, store, "album", "x", "d1", "like", false, 0, 0)
			assertReaction(t, store, "album", "x", "d1", "dislike", false, 0, 0)

			// Set a like, then try to remove dislike (wrong reaction)
			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			assertReaction(t, store, "album", "x", "d1", "dislike", false, 1, 0) // should no-op
			assertSnapshot(t, store, "album", "x", 0, 1, 0)
		})
	}
}

// ── 14. Multiple devices reacting to the same item ──

func TestStatsMultiDeviceReactions(t *testing.T) {
	for name, store := range testStores(t) {
		t.Run(name, func(t *testing.T) {
			assertView(t, store, "album", "x", "d1", 1)
			assertView(t, store, "album", "x", "d2", 2)
			assertView(t, store, "album", "x", "d3", 3)

			assertReaction(t, store, "album", "x", "d1", "like", true, 1, 0)
			assertReaction(t, store, "album", "x", "d2", "like", true, 2, 0)
			assertReaction(t, store, "album", "x", "d3", "dislike", true, 2, 1)

			// d1 switches to dislike
			assertReaction(t, store, "album", "x", "d1", "dislike", true, 1, 2)
			// d3 removes their dislike
			assertReaction(t, store, "album", "x", "d3", "dislike", false, 1, 1)

			assertSnapshot(t, store, "album", "x", 3, 1, 1)
		})
	}
}

// ── 15. Dialect SQL correctness ──

func TestDialectHelpers(t *testing.T) {
	tests := []struct {
		driver     string
		method     string
		args       []string
		wantSubstr string
	}{
		// insertIgnoreSQL
		{"sqlite", "insertIgnoreSQL", []string{"t", "a,b", "a,b"}, "INSERT OR IGNORE INTO t (a,b)"},
		{"pgx", "insertIgnoreSQL", []string{"t", "a,b", "a,b"}, "ON CONFLICT (a,b) DO NOTHING"},
		{"mysql", "insertIgnoreSQL", []string{"t", "a,b", "a,b"}, "INSERT IGNORE INTO t (a,b)"},

		// upsertCounterSQL
		{"sqlite", "upsertCounterSQL", []string{"t", "a,b", "a,b", "a = t.a + 1"}, "ON CONFLICT (a,b) DO UPDATE SET a = t.a + 1"},
		{"pgx", "upsertCounterSQL", []string{"t", "a,b", "a,b", "a = t.a + 1"}, "ON CONFLICT (a,b) DO UPDATE SET a = t.a + 1"},
		{"mysql", "upsertCounterSQL", []string{"t", "a,b", "a,b", "a = t.a + 1"}, "ON DUPLICATE KEY UPDATE a = t.a + 1"},

		// placeholders
		{"sqlite", "placeholders", nil, "?, ?, ?"},
		{"pgx", "placeholders", nil, "$1, $2, $3"},
		{"mysql", "placeholders", nil, "?, ?, ?"},

		// valueRef
		{"sqlite", "valueRef", []string{"col"}, "excluded.col"},
		{"pgx", "valueRef", []string{"col"}, "excluded.col"},
		{"mysql", "valueRef", []string{"col"}, "VALUES(col)"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.driver, tt.method), func(t *testing.T) {
			store := &sqlStatsStore{driver: tt.driver}
			var got string
			switch tt.method {
			case "insertIgnoreSQL":
				got = store.insertIgnoreSQL(tt.args[0], tt.args[1], tt.args[2])
			case "upsertCounterSQL":
				got = store.upsertCounterSQL(tt.args[0], tt.args[1], tt.args[2], tt.args[3])
			case "placeholders":
				got = store.placeholders(3)
			case "valueRef":
				got = store.valueRef(tt.args[0])
			}
			if !contains(got, tt.wantSubstr) {
				t.Fatalf("%s.%s = %q, want to contain %q", tt.driver, tt.method, got, tt.wantSubstr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ── 16. Factory function creates correct store type ──

func TestNewStatsStore(t *testing.T) {
	// memory
	cfg := Config{}
	cfg.Stats.Backend = "memory"
	store, err := newStatsStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(*memoryStatsStore); !ok {
		t.Fatalf("expected *memoryStatsStore, got %T", store)
	}

	// sqlite (default)
	cfg.Stats.Backend = ""
	cfg.Stats.SQLite.Path = filepath.Join(t.TempDir(), "test.db")
	store, err = newStatsStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(*sqlStatsStore); !ok {
		t.Fatalf("expected *sqlStatsStore, got %T", store)
	}
	store.(*sqlStatsStore).db.Close()
}

// ── 17. boolInt2 helper ──

func TestBoolInt2(t *testing.T) {
	if got := boolInt2(true); got != 1 {
		t.Fatalf("boolInt2(true) = %d, want 1", got)
	}
	if got := boolInt2(false); got != 0 {
		t.Fatalf("boolInt2(false) = %d, want 0", got)
	}
}
