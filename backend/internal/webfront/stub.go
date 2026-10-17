// Package webfront is the plain-HTTP server on a loopback address that sits
// behind a TLS-terminating reverse proxy (Caddy on all-in-one installs, the
// operator's own proxy elsewhere). It serves RouteBox's stub site, or, once
// one is set, the Telegram WEB proxy handler of the mtg fork.
package webfront

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type stub struct {
	dir string
}

// NewStub serves dir the way Caddy's file_server did for the stub site. A
// missing dir is not an error: every request is then an empty 404, so the
// front still comes up on a host that has no stub site installed.
func NewStub(dir string) http.Handler {
	return &stub{dir: dir}
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	if f, st := s.open(r.URL.Path); f != nil {
		defer func() { _ = f.Close() }()
		http.ServeContent(w, r, st.Name(), st.ModTime(), f)

		return
	}

	s.notFound(w, r)
}

// open returns a regular file for the url path, or nil. Directories, escapes
// and trailing slashes are all "not found": the stub never lists or redirects.
func (s *stub) open(urlPath string) (*os.File, os.FileInfo) {
	if urlPath == "/" {
		urlPath = "/index.html"
	}

	if !strings.HasPrefix(urlPath, "/") || strings.HasSuffix(urlPath, "/") ||
		strings.Contains(urlPath, "\x00") {
		return nil, nil
	}

	for _, part := range strings.Split(urlPath, "/") {
		if part == ".." {
			return nil, nil
		}
	}

	f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(path.Clean(urlPath))))
	if err != nil {
		return nil, nil
	}

	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		_ = f.Close()

		return nil, nil
	}

	return f, st
}

func (s *stub) notFound(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(filepath.Join(s.dir, "404.html"))
	if err != nil {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusNotFound)

		return
	}

	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusNotFound)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f)
	}
}
