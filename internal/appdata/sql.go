package appdata

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dialect is what differs between SQL databases; sqlApp holds everything else.
type dialect interface {
	// ph returns the n-th placeholder (1-based).
	ph(n int) string
	// field returns an expression for a data field as a value, for comparing with
	// a parameter of the given kind ("string", "number", "bool"), and for sorting.
	field(path []string, kind string) string
	sortField(path []string) string
	// contains returns a case-insensitive "text contains" condition.
	contains(expr, param string) string
	unique(err error) bool
}

// sqlApp is an app's data in a SQL database. Rows are scoped by app, so drivers can
// keep apps in separate files (SQLite) or in shared tables (Postgres).
type sqlApp struct {
	db  *sql.DB
	d   dialect
	app string
	now func() time.Time
	// lock serializes writes for drivers whose database allows one writer at a time.
	lock func() func()
}

func (a *sqlApp) Insert(ctx context.Context, coll, id string, data map[string]any, actor string) (Record, error) {
	if err := checkCollection(coll); err != nil {
		return Record{}, err
	}
	if id == "" {
		id = newID()
	}
	if err := checkRecordID(id); err != nil {
		return Record{}, err
	}
	body, err := encode(data)
	if err != nil {
		return Record{}, err
	}
	defer a.lock()()
	now := a.now().UTC()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, a.q(`INSERT INTO records (app, coll, id, data, version, created, updated, created_by, updated_by)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?)`), a.app, coll, id, body, stamp(now), stamp(now), actor, actor)
	if err != nil {
		if a.d.unique(err) {
			return Record{}, fmt.Errorf("%w: record %q in %s", ErrExists, id, coll)
		}
		return Record{}, err
	}
	if err := a.log(ctx, tx, coll, id, "insert", actor, nil, []byte(body), now); err != nil {
		return Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return Record{}, err
	}
	return Record{Collection: coll, ID: id, Data: data, Version: 1, Created: now, Updated: now, CreatedBy: actor, UpdatedBy: actor}, nil
}

func (a *sqlApp) Update(ctx context.Context, coll, id string, patch map[string]any, version int, actor string) (Record, error) {
	if err := checkCollection(coll); err != nil {
		return Record{}, err
	}
	defer a.lock()()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	old, err := a.get(ctx, tx, coll, id)
	if err != nil {
		return Record{}, err
	}
	if version != 0 && version != old.Version {
		return Record{}, ErrConflict
	}
	next := old
	next.Data = merge(old.Data, patch)
	body, err := encode(next.Data)
	if err != nil {
		return Record{}, err
	}
	before, _ := json.Marshal(old.Data)
	now := a.now().UTC()
	res, err := tx.ExecContext(ctx, a.q(`UPDATE records SET data = ?, version = version + 1, updated = ?, updated_by = ?
		WHERE app = ? AND coll = ? AND id = ? AND version = ?`), body, stamp(now), actor, a.app, coll, id, old.Version)
	if err != nil {
		return Record{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Record{}, ErrConflict
	}
	if err := a.log(ctx, tx, coll, id, "update", actor, before, []byte(body), now); err != nil {
		return Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return Record{}, err
	}
	next.Version++
	next.Updated, next.UpdatedBy = now, actor
	return next, nil
}

func (a *sqlApp) Delete(ctx context.Context, coll, id string, version int, actor string) error {
	if err := checkCollection(coll); err != nil {
		return err
	}
	defer a.lock()()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := a.get(ctx, tx, coll, id)
	if err != nil {
		return err
	}
	if version != 0 && version != old.Version {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, a.q(`DELETE FROM records WHERE app = ? AND coll = ? AND id = ?`), a.app, coll, id); err != nil {
		return err
	}
	before, _ := json.Marshal(old.Data)
	if err := a.log(ctx, tx, coll, id, "delete", actor, before, nil, a.now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *sqlApp) Get(ctx context.Context, coll, id string) (Record, error) {
	if err := checkCollection(coll); err != nil {
		return Record{}, err
	}
	return a.get(ctx, a.db, coll, id)
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (a *sqlApp) get(ctx context.Context, q querier, coll, id string) (Record, error) {
	r, err := scanRecord(q.QueryRowContext(ctx, a.q(`SELECT coll, id, data, version, created, updated, created_by, updated_by
		FROM records WHERE app = ? AND coll = ? AND id = ?`), a.app, coll, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("%w: record %q in %s", ErrNotFound, id, coll)
	}
	return r, err
}

func (a *sqlApp) Query(ctx context.Context, coll string, q Query) ([]Record, error) {
	if err := checkCollection(coll); err != nil {
		return nil, err
	}
	q, err := checkQuery(q)
	if err != nil {
		return nil, err
	}
	args := []any{a.app, coll}
	where := []string{"app = " + a.d.ph(1), "coll = " + a.d.ph(2)}
	param := func(v any) string {
		args = append(args, normalize(v))
		return a.d.ph(len(args))
	}
	for _, c := range q.Where {
		switch c.Op {
		case "in":
			list := c.Value.([]any)
			ps := make([]string, len(list))
			for i, v := range list {
				ps[i] = param(v)
			}
			where = append(where, a.expr(c.Field, kindOf(list[0]))+" IN ("+strings.Join(ps, ", ")+")")
		case "contains":
			where = append(where, a.d.contains(a.expr(c.Field, "string"), param(c.Value)))
		default:
			where = append(where, a.expr(c.Field, kindOf(c.Value))+" "+c.Op+" "+param(c.Value))
		}
	}
	order := make([]string, 0, len(q.OrderBy)+1)
	for _, o := range q.OrderBy {
		e := a.sortExpr(o.Field)
		if o.Desc {
			e += " DESC"
		}
		order = append(order, e)
	}
	order = append(order, "id")
	query := `SELECT coll, id, data, version, created, updated, created_by, updated_by FROM records WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY ` + strings.Join(order, ", ") +
		` LIMIT ` + strconv.Itoa(q.Limit) + ` OFFSET ` + strconv.Itoa(q.Offset)
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var systemColumns = map[string]string{"_id": "id", "_created": "created", "_updated": "updated", "_version": "version"}

func (a *sqlApp) expr(field, kind string) string {
	if col, ok := systemColumns[field]; ok {
		return col
	}
	return a.d.field(strings.Split(field, "."), kind)
}

func (a *sqlApp) sortExpr(field string) string {
	if col, ok := systemColumns[field]; ok {
		return col
	}
	return a.d.sortField(strings.Split(field, "."))
}

func kindOf(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case string:
		return "string"
	}
	return "number"
}

func (a *sqlApp) Collections(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, a.q(`SELECT DISTINCT coll FROM records WHERE app = ? ORDER BY coll`), a.app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *sqlApp) History(ctx context.Context, coll, id string, limit int) ([]Change, error) {
	if limit <= 0 || limit > maxHistory {
		limit = maxHistory
	}
	q := `SELECT seq, coll, record, action, actor, before, after, at FROM history WHERE app = ?`
	args := []any{a.app}
	if coll != "" {
		q += ` AND coll = ?`
		args = append(args, coll)
	}
	if id != "" {
		q += ` AND record = ?`
		args = append(args, id)
	}
	q += ` ORDER BY seq DESC LIMIT ` + strconv.Itoa(limit)
	return a.history(ctx, a.q(q), args...)
}

func (a *sqlApp) history(ctx context.Context, query string, args ...any) ([]Change, error) {
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Change{}
	for rows.Next() {
		var c Change
		var before, after sql.NullString
		var at any
		if err := rows.Scan(&c.Seq, &c.Collection, &c.Record, &c.Action, &c.Actor, &before, &after, &at); err != nil {
			return nil, err
		}
		if before.Valid {
			json.Unmarshal([]byte(before.String), &c.Before)
		}
		if after.Valid {
			json.Unmarshal([]byte(after.String), &c.After)
		}
		c.Time = parseTime(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *sqlApp) Export(ctx context.Context) (Snapshot, error) {
	var s Snapshot
	rows, err := a.db.QueryContext(ctx, a.q(`SELECT coll, id, data, version, created, updated, created_by, updated_by
		FROM records WHERE app = ? ORDER BY coll, id`), a.app)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			rows.Close()
			return s, err
		}
		s.Records = append(s.Records, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, err
	}
	s.History, err = a.history(ctx, a.q(`SELECT seq, coll, record, action, actor, before, after, at FROM history WHERE app = ? ORDER BY seq`), a.app)
	return s, err
}

func (a *sqlApp) Import(ctx context.Context, s Snapshot) error {
	defer a.lock()()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, a.q(`SELECT COUNT(*) FROM records WHERE app = ?`), a.app).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: app %s already has data here", ErrExists, a.app)
	}
	for _, r := range s.Records {
		body, err := encode(r.Data)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, a.q(`INSERT INTO records (app, coll, id, data, version, created, updated, created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`), a.app, r.Collection, r.ID, body, r.Version, stamp(r.Created), stamp(r.Updated), r.CreatedBy, r.UpdatedBy); err != nil {
			return err
		}
	}
	for _, c := range s.History {
		var before, after []byte
		if c.Before != nil {
			before, _ = json.Marshal(c.Before)
		}
		if c.After != nil {
			after, _ = json.Marshal(c.After)
		}
		if err := a.log(ctx, tx, c.Collection, c.Record, c.Action, c.Actor, before, after, c.Time); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *sqlApp) log(ctx context.Context, tx *sql.Tx, coll, id, action, actor string, before, after []byte, at time.Time) error {
	_, err := tx.ExecContext(ctx, a.q(`INSERT INTO history (app, coll, record, action, actor, before, after, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		a.app, coll, id, action, actor, nullable(before), nullable(after), stamp(at))
	return err
}

// q turns ? placeholders into the dialect's.
func (a *sqlApp) q(query string) string {
	if a.d.ph(1) == "?" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString(a.d.ph(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

type rowScanner interface{ Scan(...any) error }

func scanRecord(r rowScanner) (Record, error) {
	var rec Record
	var data string
	var created, updated any
	if err := r.Scan(&rec.Collection, &rec.ID, &data, &rec.Version, &created, &updated, &rec.CreatedBy, &rec.UpdatedBy); err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal([]byte(data), &rec.Data); err != nil {
		return Record{}, err
	}
	rec.Created, rec.Updated = parseTime(created), parseTime(updated)
	return rec, nil
}

func encode(data map[string]any) (string, error) {
	if data == nil {
		data = map[string]any{}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return "", invalid("data must be JSON: %v", err)
	}
	if len(b) > MaxRecordSize {
		return "", invalid("record is too big (at most %d KB)", MaxRecordSize>>10)
	}
	return string(b), nil
}

func nullable(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

// stamp is how times are stored: sortable text, the same in every driver.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func parseTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t.UTC()
	case string:
		p, _ := time.Parse(time.RFC3339Nano, t)
		return p.UTC()
	case []byte:
		p, _ := time.Parse(time.RFC3339Nano, string(t))
		return p.UTC()
	}
	return time.Time{}
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
