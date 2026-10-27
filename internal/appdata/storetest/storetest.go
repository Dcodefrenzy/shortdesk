// Package storetest holds the tests every appdata driver must pass, so switching the
// database behind apps never changes how they behave.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/shortdesk/shortdesk/internal/appdata"
)

// Run runs the shared tests; open returns a new, empty store.
func Run(t *testing.T, open func(t *testing.T) appdata.Store) {
	ctx := context.Background()
	app := func(t *testing.T, s appdata.Store, id string) appdata.AppData {
		t.Helper()
		a, err := s.App(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	t.Run("InsertGetUpdateDelete", func(t *testing.T) {
		a := app(t, open(t), "tasks")
		r, err := a.Insert(ctx, "items", "t1", map[string]any{"title": "Call landlord", "done": false, "n": 2.0}, "agent on one")
		if err != nil || r.Version != 1 || r.CreatedBy != "agent on one" {
			t.Fatalf("insert = %+v, %v", r, err)
		}
		if _, err := a.Insert(ctx, "items", "t1", map[string]any{}, "you"); !errors.Is(err, appdata.ErrExists) {
			t.Errorf("duplicate insert err = %v", err)
		}
		r, err = a.Update(ctx, "items", "t1", map[string]any{"done": true, "n": nil}, 1, "you")
		if err != nil || r.Version != 2 || r.Data["done"] != true || r.Data["title"] != "Call landlord" {
			t.Fatalf("update = %+v, %v", r, err)
		}
		if _, ok := r.Data["n"]; ok {
			t.Errorf("nil didn't remove the key: %+v", r.Data)
		}
		if _, err := a.Update(ctx, "items", "t1", map[string]any{"done": false}, 1, "agent on one"); !errors.Is(err, appdata.ErrConflict) {
			t.Errorf("stale update err = %v, want conflict", err)
		}
		got, err := a.Get(ctx, "items", "t1")
		if err != nil || got.Version != 2 || got.Data["done"] != true || got.UpdatedBy != "you" {
			t.Fatalf("get = %+v, %v", got, err)
		}
		if err := a.Delete(ctx, "items", "t1", 1, "you"); !errors.Is(err, appdata.ErrConflict) {
			t.Errorf("stale delete err = %v", err)
		}
		if err := a.Delete(ctx, "items", "t1", 2, "you"); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Get(ctx, "items", "t1"); !errors.Is(err, appdata.ErrNotFound) {
			t.Errorf("get after delete err = %v", err)
		}
		h, err := a.History(ctx, "items", "t1", 0)
		if err != nil || len(h) != 3 || h[0].Action != "delete" || h[2].Action != "insert" || h[0].Before["title"] != "Call landlord" {
			t.Errorf("history = %+v, %v", h, err)
		}
	})

	t.Run("Query", func(t *testing.T) {
		a := app(t, open(t), "flats")
		for i, f := range []map[string]any{
			{"title": "Studio, Wedding", "price": 780.0, "status": "applied", "area": map[string]any{"city": "Berlin"}},
			{"title": "2-room, Mitte", "price": 1195.0, "status": "viewing", "area": map[string]any{"city": "Berlin"}},
			{"title": "1-room, Kreuzberg", "price": 950.0, "status": "offer", "area": map[string]any{"city": "Berlin"}, "pets": true},
			{"title": "Loft, Altona", "price": 1400.0, "status": "applied", "area": map[string]any{"city": "Hamburg"}},
		} {
			if _, err := a.Insert(ctx, "flats", fmt.Sprintf("f%d", i), f, "you"); err != nil {
				t.Fatal(err)
			}
		}
		titles := func(q appdata.Query) []string {
			t.Helper()
			rs, err := a.Query(ctx, "flats", q)
			if err != nil {
				t.Fatal(err)
			}
			out := []string{}
			for _, r := range rs {
				out = append(out, r.Data["title"].(string))
			}
			return out
		}
		eq := func(name string, got []string, want ...string) {
			t.Helper()
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("%s = %v, want %v", name, got, want)
			}
		}
		eq("under 1000 by price", titles(appdata.Query{Where: []appdata.Cond{{Field: "price", Op: "<", Value: 1000}}, OrderBy: []appdata.Order{{Field: "price"}}}),
			"Studio, Wedding", "1-room, Kreuzberg")
		eq("status in", titles(appdata.Query{Where: []appdata.Cond{{Field: "status", Op: "in", Value: []any{"offer", "viewing"}}}, OrderBy: []appdata.Order{{Field: "title"}}}),
			"1-room, Kreuzberg", "2-room, Mitte")
		eq("contains, any case", titles(appdata.Query{Where: []appdata.Cond{{Field: "title", Op: "contains", Value: "WEDDING"}}}), "Studio, Wedding")
		eq("nested field", titles(appdata.Query{Where: []appdata.Cond{{Field: "area.city", Op: "=", Value: "Hamburg"}}}), "Loft, Altona")
		eq("bool", titles(appdata.Query{Where: []appdata.Cond{{Field: "pets", Op: "=", Value: true}}}), "1-room, Kreuzberg")
		eq("not equal + desc + limit", titles(appdata.Query{Where: []appdata.Cond{{Field: "status", Op: "!=", Value: "applied"}}, OrderBy: []appdata.Order{{Field: "price", Desc: true}}, Limit: 1}),
			"2-room, Mitte")
		eq("system field", titles(appdata.Query{Where: []appdata.Cond{{Field: "_id", Op: "=", Value: "f3"}}}), "Loft, Altona")
		eq("offset", titles(appdata.Query{OrderBy: []appdata.Order{{Field: "price"}}, Limit: 2, Offset: 2}), "2-room, Mitte", "Loft, Altona")
		for name, q := range map[string]appdata.Query{
			"bad field":    {Where: []appdata.Cond{{Field: "price); DROP TABLE records;--", Op: "=", Value: 1}}},
			"bad op":       {Where: []appdata.Cond{{Field: "price", Op: "LIKE", Value: "%"}}},
			"bad value":    {Where: []appdata.Cond{{Field: "price", Op: "=", Value: []any{1}}}},
			"unknown _sys": {Where: []appdata.Cond{{Field: "_secret", Op: "=", Value: 1}}},
		} {
			if _, err := a.Query(ctx, "flats", q); !errors.Is(err, appdata.ErrInvalid) {
				t.Errorf("%s: err = %v, want invalid", name, err)
			}
		}
		colls, err := a.Collections(ctx)
		if err != nil || fmt.Sprint(colls) != "[flats]" {
			t.Errorf("collections = %v, %v", colls, err)
		}
	})

	t.Run("AppsAreSeparate", func(t *testing.T) {
		s := open(t)
		a, b := app(t, s, "one"), app(t, s, "two")
		if _, err := a.Insert(ctx, "notes", "n1", map[string]any{"text": "mine"}, "you"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Get(ctx, "notes", "n1"); !errors.Is(err, appdata.ErrNotFound) {
			t.Errorf("another app sees the record: %v", err)
		}
		if rs, _ := b.Query(ctx, "notes", appdata.Query{}); len(rs) != 0 {
			t.Errorf("another app's query = %+v", rs)
		}
		if err := s.DropApp(ctx, "one"); err != nil {
			t.Fatal(err)
		}
		a = app(t, s, "one")
		if rs, _ := a.Query(ctx, "notes", appdata.Query{}); len(rs) != 0 {
			t.Errorf("data survived DropApp: %+v", rs)
		}
	})

	t.Run("InputsAreChecked", func(t *testing.T) {
		s := open(t)
		if _, err := s.App(ctx, "../etc"); !errors.Is(err, appdata.ErrInvalid) {
			t.Errorf("bad app id err = %v", err)
		}
		a := app(t, s, "tasks")
		if _, err := a.Insert(ctx, "Bad Name", "", map[string]any{}, "you"); !errors.Is(err, appdata.ErrInvalid) {
			t.Errorf("bad collection err = %v", err)
		}
		if _, err := a.Insert(ctx, "items", "a/b", map[string]any{}, "you"); !errors.Is(err, appdata.ErrInvalid) {
			t.Errorf("bad record id err = %v", err)
		}
		big := map[string]any{"x": string(make([]byte, appdata.MaxRecordSize+1))}
		if _, err := a.Insert(ctx, "items", "", big, "you"); !errors.Is(err, appdata.ErrInvalid) {
			t.Errorf("too big err = %v", err)
		}
		r, err := a.Insert(ctx, "items", "", map[string]any{"x": 1}, "you")
		if err != nil || r.ID == "" {
			t.Errorf("made-up id = %q, %v", r.ID, err)
		}
	})

	t.Run("CopyMovesEverything", func(t *testing.T) {
		src, dst := open(t), open(t)
		a := app(t, src, "tasks")
		a.Insert(ctx, "items", "t1", map[string]any{"title": "one"}, "you")
		a.Update(ctx, "items", "t1", map[string]any{"title": "one!"}, 0, "agent on one")
		app(t, src, "notes").Insert(ctx, "notes", "n1", map[string]any{"text": "hi"}, "you")
		if err := appdata.Copy(ctx, src, dst); err != nil {
			t.Fatal(err)
		}
		got, err := app(t, dst, "tasks").Get(ctx, "items", "t1")
		if err != nil || got.Version != 2 || got.Data["title"] != "one!" || got.UpdatedBy != "agent on one" {
			t.Errorf("copied record = %+v, %v", got, err)
		}
		if h, _ := app(t, dst, "tasks").History(ctx, "items", "t1", 0); len(h) != 2 {
			t.Errorf("copied history = %+v", h)
		}
		if _, err := app(t, dst, "notes").Get(ctx, "notes", "n1"); err != nil {
			t.Errorf("second app not copied: %v", err)
		}
		if err := appdata.Copy(ctx, src, dst); !errors.Is(err, appdata.ErrExists) {
			t.Errorf("copying over existing data err = %v", err)
		}
	})
}
