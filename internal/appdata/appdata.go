// Package appdata stores the data of ShortApps. Apps and agents only ever use the
// Store and AppData interfaces, never a database directly, so the database behind
// them can change: SQLite (one file per app) now, another database later. Every
// driver passes the same tests (storetest), and Copy moves all data between two
// stores, which is how a workspace switches.
//
// An app's data is a set of collections of JSON records. Every record has a version
// that goes up on each change, so a writer holding an old version can't overwrite a
// newer change, and every change is kept in the app's history.
package appdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("the record was changed by someone else; read it again")
	ErrExists   = errors.New("already exists")
	ErrInvalid  = errors.New("invalid")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Store holds the data of every app in a workspace. Apps are kept apart: an AppData
// only ever sees its own app.
type Store interface {
	// App returns an app's data, creating its space on first use.
	App(ctx context.Context, appID string) (AppData, error)
	// Apps lists the apps that have data.
	Apps(ctx context.Context) ([]string, error)
	// DropApp deletes an app's data and history.
	DropApp(ctx context.Context, appID string) error
	Close() error
}

// AppData is one app's records.
type AppData interface {
	// Insert adds a record; id is made up when empty.
	Insert(ctx context.Context, collection, id string, data map[string]any, actor string) (Record, error)
	// Update merges patch into a record; keys set to nil are removed. A non-zero
	// version must match the record's.
	Update(ctx context.Context, collection, id string, patch map[string]any, version int, actor string) (Record, error)
	// Delete removes a record; a non-zero version must match. It stays in the history.
	Delete(ctx context.Context, collection, id string, version int, actor string) error
	Get(ctx context.Context, collection, id string) (Record, error)
	Query(ctx context.Context, collection string, q Query) ([]Record, error)
	Collections(ctx context.Context) ([]string, error)
	// History returns changes, newest first: the whole app's, or one record's when id is set.
	History(ctx context.Context, collection, id string, limit int) ([]Change, error)

	// Export and Import move everything, versions and history included, between stores.
	Export(ctx context.Context) (Snapshot, error)
	Import(ctx context.Context, s Snapshot) error
}

// Record is one item in a collection.
type Record struct {
	Collection string         `json:"collection"`
	ID         string         `json:"id"`
	Data       map[string]any `json:"data"`
	Version    int            `json:"version"`
	Created    time.Time      `json:"created"`
	Updated    time.Time      `json:"updated"`
	CreatedBy  string         `json:"createdBy"`
	UpdatedBy  string         `json:"updatedBy"`
}

// Change is one entry in an app's history.
type Change struct {
	Seq        int64          `json:"seq"`
	Collection string         `json:"collection"`
	Record     string         `json:"record"`
	Action     string         `json:"action"` // insert, update, delete
	Actor      string         `json:"actor"`
	Before     map[string]any `json:"before,omitempty"`
	After      map[string]any `json:"after,omitempty"`
	Time       time.Time      `json:"time"`
}

// Snapshot is all of an app's data, for moving it between stores.
type Snapshot struct {
	Records []Record `json:"records"`
	History []Change `json:"history"`
}

// Query selects records. Fields are keys in the record's data (dots reach into
// nested objects), or _id, _created, _updated and _version.
type Query struct {
	Where   []Cond
	OrderBy []Order // default: most recently updated first
	Limit   int     // default and maximum: MaxQuery
	Offset  int
}

// Cond compares a field with a value. Ops: = != < <= > >= in contains.
type Cond struct {
	Field string
	Op    string
	Value any // a string, number or bool; a list for "in"
}

type Order struct {
	Field string
	Desc  bool
}

const (
	MaxQuery      = 1000
	MaxRecordSize = 256 << 10
	maxHistory    = 500
)

var (
	appIDRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	collectionRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	recordIDRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	fieldRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}(\.[A-Za-z_][A-Za-z0-9_]{0,63}){0,3}$`)
)

var systemFields = map[string]bool{"_id": true, "_created": true, "_updated": true, "_version": true}

func checkApp(id string) error {
	if !appIDRe.MatchString(id) {
		return invalid("app id %q: use lower case letters, digits and dashes", id)
	}
	return nil
}

func checkCollection(c string) error {
	if !collectionRe.MatchString(c) {
		return invalid("collection %q: use lower case letters, digits and underscores", c)
	}
	return nil
}

func checkRecordID(id string) error {
	if !recordIDRe.MatchString(id) {
		return invalid("record id %q: use letters, digits, - and _", id)
	}
	return nil
}

// checkQuery validates a query and fills in its defaults.
func checkQuery(q Query) (Query, error) {
	for _, c := range q.Where {
		if !fieldRe.MatchString(c.Field) {
			return q, invalid("field %q", c.Field)
		}
		if strings.HasPrefix(c.Field, "_") && !systemFields[c.Field] {
			return q, invalid("unknown field %q", c.Field)
		}
		switch c.Op {
		case "=", "!=", "<", "<=", ">", ">=":
			if !scalar(c.Value) {
				return q, invalid("%s %s needs a string, number or true/false", c.Field, c.Op)
			}
		case "contains":
			if _, ok := c.Value.(string); !ok {
				return q, invalid("%s contains needs text", c.Field)
			}
		case "in":
			list, ok := c.Value.([]any)
			if !ok || len(list) == 0 || len(list) > 100 {
				return q, invalid("%s in needs a list of 1 to 100 values", c.Field)
			}
			for _, v := range list {
				if !scalar(v) {
					return q, invalid("%s in: values must be strings, numbers or true/false", c.Field)
				}
			}
		default:
			return q, invalid("unknown operator %q", c.Op)
		}
	}
	for _, o := range q.OrderBy {
		if !fieldRe.MatchString(o.Field) || (strings.HasPrefix(o.Field, "_") && !systemFields[o.Field]) {
			return q, invalid("can't sort by %q", o.Field)
		}
	}
	if len(q.OrderBy) == 0 {
		q.OrderBy = []Order{{Field: "_updated", Desc: true}}
	}
	if q.Limit <= 0 || q.Limit > MaxQuery {
		q.Limit = MaxQuery
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	return q, nil
}

func scalar(v any) bool {
	switch v.(type) {
	case string, float64, int, int64, bool, json.Number:
		return true
	}
	return false
}

// normalize turns ints into float64, as JSON does, so drivers compare like with like.
func normalize(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	return v
}

// merge applies a patch: nil removes a key.
func merge(old, patch map[string]any) map[string]any {
	out := make(map[string]any, len(old)+len(patch))
	for k, v := range old {
		out[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

// Copy moves every app's data from src to dst, versions and history included. dst
// must not have data for those apps yet. It's how a workspace switches databases.
func Copy(ctx context.Context, src, dst Store) error {
	apps, err := src.Apps(ctx)
	if err != nil {
		return err
	}
	sort.Strings(apps)
	for _, id := range apps {
		from, err := src.App(ctx, id)
		if err != nil {
			return err
		}
		snap, err := from.Export(ctx)
		if err != nil {
			return fmt.Errorf("export %s: %w", id, err)
		}
		to, err := dst.App(ctx, id)
		if err != nil {
			return err
		}
		if err := to.Import(ctx, snap); err != nil {
			return fmt.Errorf("import %s: %w", id, err)
		}
	}
	return nil
}

// Open opens a store from a URL:
//
//	sqlite:///path/to/dir   one SQLite file per app in dir
//
// Another database is added as another driver that passes storetest; Copy then moves
// a workspace's data over.
func Open(url string) (Store, error) {
	switch {
	case strings.HasPrefix(url, "sqlite://"):
		return OpenSQLite(strings.TrimPrefix(url, "sqlite://"))
	}
	return nil, fmt.Errorf("unknown app database %q (use sqlite:///dir)", url)
}
