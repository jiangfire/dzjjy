package staticserve

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Site struct {
	root     string
	listener net.Listener
	server   *http.Server
	once     sync.Once
}

func Start(bindAddress, root string, port int) (*Site, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("port must be between 0 and 65535")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(bindAddress, fmt.Sprint(port)))
	if err != nil {
		return nil, fmt.Errorf("failed to bind static site port: %w", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	site := &Site{root: absRoot, listener: listener}
	mux := http.NewServeMux()
	mux.HandleFunc("/", site.serveFile)
	site.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = site.server.Serve(listener) }()
	return site, nil
}

func (s *Site) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *Site) Root() string {
	return s.root
}

func (s *Site) Close() error {
	var err error
	s.once.Do(func() {
		err = s.server.Close()
	})
	return err
}

func (s *Site) serveFile(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "\\") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
	if clean == "." || clean == string(filepath.Separator) {
		clean = "index.html"
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		path = filepath.Join(s.root, "index.html")
		resolvedPath = path
	}
	resolvedRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		http.Error(w, "site unavailable", http.StatusInternalServerError)
		return
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(resolvedPath)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(resolvedPath) // #nosec G304 - resolved path is constrained under site root
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()
	http.ServeContent(w, r, filepath.Base(resolvedPath), info.ModTime(), file)
}
