package server

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

// outputsDir is where agents save results worth keeping (see the agent's instructions).
const outputsDir = "/home/desk/outputs"

type outputJSON struct {
	Path     string    `json:"path"` // relative to the outputs folder
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Type     string    `json:"type"` // content type guessed from the extension
}

// listOutputs returns the files in the desk's outputs folder, newest first.
func (s *Server) listOutputs(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	var out bytes.Buffer
	err := s.desks.Runtime().Exec(r.Context(), d.ID, runtime.ExecRequest{
		Command: `mkdir -p ` + outputsDir + ` && find ` + outputsDir + ` -maxdepth 3 -type f ! -name '.*' -printf '%P\t%s\t%T@\n' | head -500`,
		Stdout:  &out,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	files := []outputJSON{}
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), "\t")
		if len(parts) != 3 || parts[0] == "" {
			continue
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		secs, _ := strconv.ParseFloat(parts[2], 64)
		files = append(files, outputJSON{
			Path:     parts[0],
			Size:     size,
			Modified: time.Unix(0, int64(secs*float64(time.Second))).UTC(),
			Type:     contentType(parts[0]),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Modified.After(files[j].Modified) })
	writeJSON(w, http.StatusOK, files)
}

// getOutput serves one file from the outputs folder. Agent-made files are untrusted:
// they're served under a sandbox CSP, so even an HTML page opened directly can't run as
// part of the dashboard, and HTML/SVG are downloads unless shown in the sandboxed preview.
func (s *Server) getOutput(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	rel, err := cleanOutputPath(r.PathValue("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	files, ok := s.desks.Runtime().(runtime.Files)
	if !ok {
		writeError(w, http.StatusNotImplemented, errors.New("this runtime can't read desk files"))
		return
	}
	var buf bytes.Buffer
	if err := files.Download(r.Context(), d.ID, outputsDir+"/"+rel, &buf); err != nil {
		writeError(w, http.StatusNotFound, errors.New("file not found"))
		return
	}
	ct := contentType(rel)
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'")
	h.Set("Cache-Control", "no-store")
	name := path.Base(rel)
	if r.URL.Query().Get("download") != "" || (risky(ct) && r.URL.Query().Get("preview") == "") {
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	} else {
		h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	}
	w.Write(buf.Bytes())
}

// cleanOutputPath keeps a requested path inside the outputs folder.
func cleanOutputPath(p string) (string, error) {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("invalid file path %q", p)
		}
	}
	clean := path.Clean("/" + p)[1:]
	if clean == "" || clean == "." || strings.HasPrefix(clean, "..") || strings.Contains(clean, "/.") || strings.HasPrefix(clean, ".") {
		return "", fmt.Errorf("invalid file path %q", p)
	}
	return clean, nil
}

func contentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".txt", ".log":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	}
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// risky types can run scripts when opened as a page.
func risky(ct string) bool {
	return strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "image/svg") || strings.Contains(ct, "xml")
}
