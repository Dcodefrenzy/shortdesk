package appdata

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure Go, so every platform builds without a C compiler
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS records (
	app        TEXT NOT NULL,
	coll       TEXT NOT NULL,
	id         TEXT NOT NULL,
	data       TEXT NOT NULL,
	version    INTEGER NOT NULL,
	created    TEXT NOT NULL,
	updated    TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	PRIMARY KEY (app, coll, id)
);
CREATE TABLE IF NOT EXISTS history (
	seq    INTEGER PRIMARY KEY AUTOINCREMENT,
	app    TEXT NOT NULL,
	coll   TEXT NOT NULL,
	record TEXT NOT NULL,
	action TEXT NOT NULL,
	actor  TEXT NOT NULL,
	before TEXT,
	after  TEXT,
	at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS history_by_record ON history (app, coll, record, seq);
`

// sqliteStore keeps each app in its own SQLite file in dir, so apps can't see each
// other's data and one app can be copied or deleted on its own.
type sqliteStore struct {
	dir  string
	mu   sync.Mutex
	apps map[string]*sqlApp
	dbs  map[string]*sql.DB
	now  func() time.Time
}

// OpenSQLite opens the store in dir, creating it if needed.
func OpenSQLite(dir string) (Store, error) {
	if dir == "" {
		return nil, invalid("an SQLite store needs a folder")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &sqliteStore{dir: dir, apps: map[string]*sqlApp{}, dbs: map[string]*sql.DB{}, now: time.Now}, nil
}

func (s *sqliteStore) file(appID string) string { return filepath.Join(s.dir, appID+".db") }

func (s *sqliteStore) App(ctx context.Context, appID string) (AppData, error) {
	if err := checkApp(appID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.apps[appID]; ok {
		return a, nil
	}
	db, err := sql.Open("sqlite", "file:"+s.file(appID)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// One connection: SQLite allows one writer, and transactions then never wait on
	// each other inside this process.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, sqliteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("app %s data: %w", appID, err)
	}
	var mu sync.Mutex
	a := &sqlApp{db: db, d: sqliteDialect{}, app: appID, now: func() time.Time { return s.now() },
		lock: func() func() { mu.Lock(); return mu.Unlock }}
	s.apps[appID], s.dbs[appID] = a, db
	return a, nil
}

func (s *sqliteStore) Apps(_ context.Context) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(s.dir, "*.db"))
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".db")
		if checkApp(id) == nil {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *sqliteStore) DropApp(_ context.Context, appID string) error {
	if err := checkApp(appID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if db, ok := s.dbs[appID]; ok {
		db.Close()
		delete(s.dbs, appID)
		delete(s.apps, appID)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(s.file(appID) + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *sqliteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	for id, db := range s.dbs {
		if err := db.Close(); err != nil && first == nil {
			first = err
		}
		delete(s.dbs, id)
		delete(s.apps, id)
	}
	return first
}

type sqliteDialect struct{}

func (sqliteDialect) ph(int) string { return "?" }

// jsonPath turns checked field parts into an SQLite JSON path ($.a.b). Parts only
// contain letters, digits and underscores (fieldRe), so they are safe to inline.
func jsonPath(path []string) string { return "'$." + strings.Join(path, ".") + "'" }

func (sqliteDialect) field(path []string, _ string) string {
	return "json_extract(data, " + jsonPath(path) + ")"
}

func (sqliteDialect) sortField(path []string) string {
	return "json_extract(data, " + jsonPath(path) + ")"
}

func (sqliteDialect) contains(expr, param string) string {
	return "instr(lower(CAST(" + expr + " AS TEXT)), lower(" + param + ")) > 0"
}

func (sqliteDialect) unique(err error) bool { return strings.Contains(err.Error(), "UNIQUE") }
