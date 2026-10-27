package webui

import (
	"io/fs"
	"testing"
)

func TestFSHasIndex(t *testing.T) {
	if _, err := fs.Stat(FS(), "index.html"); err != nil {
		t.Fatal(err)
	}
}
