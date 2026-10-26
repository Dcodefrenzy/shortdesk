package opensandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

var _ runtime.Files = (*Runtime)(nil)

// Upload streams r to path inside the sandbox through execd's multipart upload:
// a JSON metadata part followed by the file part.
func (r *Runtime) Upload(ctx context.Context, id, path string, src io.Reader) error {
	ep, err := r.Endpoint(ctx, id, execdPort)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		// execd expects the metadata as a file part with a JSON content type, not a plain field.
		meta, _ := json.Marshal(map[string]any{"path": path, "mode": 600})
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="metadata"; filename="metadata"`)
		h.Set("Content-Type", "application/json")
		mpart, err := mw.CreatePart(h)
		if err == nil {
			_, err = mpart.Write(meta)
		}
		if err == nil {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", `form-data; name="file"; filename="file"`)
			h.Set("Content-Type", "application/octet-stream")
			var part io.Writer
			if part, err = mw.CreatePart(h); err == nil {
				if _, err = io.Copy(part, src); err == nil {
					err = mw.Close()
				}
			}
		}
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL()+"/files/upload", pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := (&http.Client{Transport: r.http.Transport}).Do(req)
	if err != nil {
		return fmt.Errorf("upload %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upload %s: %w", path, &apiError{Status: resp.StatusCode, Body: string(b)})
	}
	return nil
}

// Download copies a file out of the sandbox.
func (r *Runtime) Download(ctx context.Context, id, path string, w io.Writer) error {
	ep, err := r.Endpoint(ctx, id, execdPort)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.URL()+"/files/download?path="+url.QueryEscape(path), nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: r.http.Transport}).Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download %s: %w", path, &apiError{Status: resp.StatusCode, Body: string(b)})
	}
	_, err = io.Copy(w, resp.Body)
	return err
}
