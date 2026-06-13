package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type sqlStatsStore struct {
	db     *sql.DB
	driver string // "sqlite", "pgx", "mysql"
}

func newSQLStatsStore(driver, dsn string) (*sqlStatsStore, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("sql stats: open %s: %w", driver, err)
	}

	// Sensible pool defaults for a low-traffic gallery
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sql stats: ping %s: %w", driver, err)
	}

	store := &sqlStatsStore{db: db, driver: driver}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sql stats: migrate %s: %w", driver, err)
	}
	return store, nil
}

func (s *sqlStatsStore) migrate(ctx context.Context) error {
	// SQLite optimizations for concurrent access
	if s.driver == "sqlite" {
		for _, pragma := range []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA busy_timeout=5000",
			"PRAGMA synchronous=NORMAL",
		} {
			if _, err := s.db.ExecContext(ctx, pragma); err != nil {
				return fmt.Errorf("migrate pragma: %w", err)
			}
		}
	}

	// All three tables use the same schema across SQLite / pg / mysql
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS item_stats (
			target_type TEXT NOT NULL,
			target_id   TEXT NOT NULL,
			views       INTEGER NOT NULL DEFAULT 0,
			likes       INTEGER NOT NULL DEFAULT 0,
			dislikes    INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (target_type, target_id)
		)`,
		`CREATE TABLE IF NOT EXISTS device_views (
			device_id   TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id   TEXT NOT NULL,
			PRIMARY KEY (device_id, target_type, target_id)
		)`,
		`CREATE TABLE IF NOT EXISTS device_reactions (
			device_id   TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id   TEXT NOT NULL,
			reaction    TEXT NOT NULL,
			PRIMARY KEY (device_id, target_type, target_id)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w\n%s", err, stmt)
		}
	}
	return nil
}

// ── Dialect helpers ──

// insertIgnore returns SQL for inserting a row, ignoring on conflict.
// Returns (prefix, suffix) — caller places VALUES between them.
func (s *sqlStatsStore) insertIgnoreSQL(table, cols, conflictCols string) string {
	switch s.driver {
	case "sqlite":
		return fmt.Sprintf("INSERT OR IGNORE INTO %s (%s)", table, cols)
	case "mysql":
		return fmt.Sprintf("INSERT IGNORE INTO %s (%s)", table, cols)
	default: // pgx
		return fmt.Sprintf("INSERT INTO %s (%s) ON CONFLICT (%s) DO NOTHING", table, cols, conflictCols)
	}
}

// upsertSQL returns SQL for upserting a row — insert or update counters.
func (s *sqlStatsStore) upsertCounterSQL(table, cols, conflictCols, updateSet string) string {
	switch s.driver {
	case "mysql":
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
			table, cols, s.placeholders(strings.Count(cols, ",")+1), updateSet)
	default: // pgx, sqlite
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
			table, cols, s.placeholders(strings.Count(cols, ",")+1), conflictCols, updateSet)
	}
}

// upsertSetSQL returns SQL for upserting and setting specific columns.
func (s *sqlStatsStore) upsertSetSQL(table, cols, conflictCols, setCols string) string {
	switch s.driver {
	case "mysql":
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
			table, cols, s.placeholders(strings.Count(cols, ",")+1), setCols)
	default: // pgx, sqlite
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
			table, cols, s.placeholders(strings.Count(cols, ",")+1), conflictCols, setCols)
	}
}

func (s *sqlStatsStore) placeholders(n int) string {
	if s.driver == "pgx" {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf("$%d", i+1)
		}
		return strings.Join(parts, ", ")
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// retryOnBusy retries a function on SQLite "database is locked" errors.
// SQLite serializes writers; we retry up to maxAttempts with backoff.
func (s *sqlStatsStore) retryOnBusy(fn func() error) error {
	const maxAttempts = 10
	const backoff = 10 * time.Millisecond
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if s.driver != "sqlite" {
			return err
		}
		// Only retry on lock errors
		if !isSQLiteBusy(err) {
			return err
		}
		time.Sleep(backoff * time.Duration(attempt+1))
	}
	return err
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsStr(msg, "database is locked") || containsStr(msg, "SQLITE_BUSY")
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && indexSubstr(s, substr) >= 0
}

func indexSubstr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ── StatsStore implementation ──

func (s *sqlStatsStore) Snapshot(targetType, targetID string) itemStats {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var stats itemStats
	err := s.db.QueryRowContext(ctx,
		`SELECT views, likes, dislikes FROM item_stats WHERE target_type=? AND target_id=?`,
		targetType, targetID,
	).Scan(&stats.Views, &stats.Likes, &stats.Dislikes)
	if err != nil {
		return itemStats{}
	}
	return stats
}

func (s *sqlStatsStore) IncrementView(targetType, targetID, deviceID string) itemStats {
	var result itemStats
	s.retryOnBusy(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Try to record this device view; no-op if already seen
		insertSQL := s.insertIgnoreSQL("device_views", "device_id, target_type, target_id", "device_id, target_type, target_id")
		insertSQL += " VALUES (" + s.placeholders(3) + ")"
		res, err := tx.ExecContext(ctx, insertSQL, deviceID, targetType, targetID)
		if err != nil {
			result = s.readStats(ctx, tx, targetType, targetID)
			return nil // duplicate is not a retryable error
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			result = s.readStats(ctx, tx, targetType, targetID)
			return nil
		}

		// First view — upsert with views = 1 or increment
		upsertSQL := s.upsertCounterSQL("item_stats",
			"target_type, target_id, views, likes, dislikes",
			"target_type, target_id",
			"views = item_stats.views + 1")
		if _, err := tx.ExecContext(ctx, upsertSQL, targetType, targetID, 1, 0, 0); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
		result = s.Snapshot(targetType, targetID)
		return nil
	})
	return result
}

func (s *sqlStatsStore) IncrementReaction(targetType, targetID, deviceID, reaction string, active bool) (itemStats, error) {
	if reaction != "like" && reaction != "dislike" {
		return itemStats{}, fmt.Errorf("%w: unsupported reaction", errBadRequest)
	}

	var result itemStats
	var resultErr error
	s.retryOnBusy(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Read current device reaction
		var existingReaction sql.NullString
		_ = tx.QueryRowContext(ctx,
			`SELECT reaction FROM device_reactions WHERE device_id=? AND target_type=? AND target_id=?`,
			deviceID, targetType, targetID,
		).Scan(&existingReaction)

		existing := ""
		if existingReaction.Valid {
			existing = existingReaction.String
		}

		if active {
			if existing == reaction {
				result = s.readStats(ctx, tx, targetType, targetID)
				return nil
			}
			// Decrement old reaction
			if existing == "like" || existing == "dislike" {
				s.decrementReaction(ctx, tx, targetType, targetID, existing)
			}
			// Upsert device reaction
			upsertSQL := s.upsertSetSQL("device_reactions",
				"device_id, target_type, target_id, reaction",
				"device_id, target_type, target_id",
				"reaction="+s.valueRef("reaction"))
			_, _ = tx.ExecContext(ctx, upsertSQL, deviceID, targetType, targetID, reaction)
			// Increment new reaction
			upsertStatsSQL := s.upsertCounterSQL("item_stats",
				"target_type, target_id, views, likes, dislikes",
				"target_type, target_id",
				s.reactionCol(reaction)+" = item_stats."+s.reactionCol(reaction)+" + 1")
			_, _ = tx.ExecContext(ctx, upsertStatsSQL, targetType, targetID, 0, boolInt2(reaction == "like"), boolInt2(reaction == "dislike"))
		} else {
			if existing != reaction {
				result = s.readStats(ctx, tx, targetType, targetID)
				return nil
			}
			// Delete device reaction
			_, _ = tx.ExecContext(ctx,
				`DELETE FROM device_reactions WHERE device_id=? AND target_type=? AND target_id=?`,
				deviceID, targetType, targetID)
			// Decrement reaction count
			s.decrementReaction(ctx, tx, targetType, targetID, reaction)
		}

		if err := tx.Commit(); err != nil {
			return err
		}
		result = s.Snapshot(targetType, targetID)
		return nil
	})
	return result, resultErr
}

func (s *sqlStatsStore) readStats(ctx context.Context, tx *sql.Tx, targetType, targetID string) itemStats {
	var stats itemStats
	err := tx.QueryRowContext(ctx,
		`SELECT views, likes, dislikes FROM item_stats WHERE target_type=? AND target_id=?`,
		targetType, targetID,
	).Scan(&stats.Views, &stats.Likes, &stats.Dislikes)
	if err != nil {
		return itemStats{}
	}
	return stats
}

func (s *sqlStatsStore) decrementReaction(ctx context.Context, tx *sql.Tx, targetType, targetID, reaction string) {
	col := s.reactionCol(reaction)
	var query string
	if s.driver == "sqlite" {
		query = fmt.Sprintf(`UPDATE item_stats SET %s = MAX(%s - 1, 0) WHERE target_type=? AND target_id=?`, col, col)
	} else {
		query = fmt.Sprintf(`UPDATE item_stats SET %s = GREATEST(%s - 1, 0) WHERE target_type=? AND target_id=?`, col, col)
	}
	_, _ = tx.ExecContext(ctx, query, targetType, targetID)
}

func (s *sqlStatsStore) reactionCol(reaction string) string {
	if reaction == "like" {
		return "likes"
	}
	return "dislikes"
}

// valueRef returns the right-hand-side reference for a column value in upsert SET.
// pgx/sqlite use EXCLUDED.col, mysql uses VALUES(col).
func (s *sqlStatsStore) valueRef(col string) string {
	if s.driver == "mysql" {
		return fmt.Sprintf("VALUES(%s)", col)
	}
	return "excluded." + col
}

func boolInt2(b bool) int {
	if b {
		return 1
	}
	return 0
}
