package appdata_test

import (
	"testing"

	"github.com/shortdesk/shortdesk/internal/appdata"
	"github.com/shortdesk/shortdesk/internal/appdata/storetest"
)

func TestSQLite(t *testing.T) {
	storetest.Run(t, func(t *testing.T) appdata.Store {
		s, err := appdata.Open("sqlite://" + t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}
