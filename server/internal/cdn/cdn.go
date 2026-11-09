package cdn

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server serves the hot-update bundles over plain HTTP. The client builds
// bundle URLs at runtime from {App.WebServerConfig.Path} in the addressables
// catalog, so the request layout only becomes known at runtime; requests are
// therefore resolved by basename against the cdn root (flat match). On a miss
// the file is pulled from the official CDN (OriginBase), cached on disk and
// served - a transparent origin proxy so the client never 404s while the
// full bundle set is still syncing.
type Server struct {
	Root       string
	OriginBase string // e.g. https://seresjp.feimogames.com/INT_STEAM/3.3.0/026 ; empty disables origin fetch

	mu        sync.RWMutex
	cache     map[string]string
	inflight  map[string]*sync.Mutex
	client    *http.Client
}

func New(root, originBase string) *Server {
	return &Server{
		Root:       root,
		OriginBase: originBase,
		cache:      make(map[string]string),
		inflight:   make(map[string]*sync.Mutex),
		client:     &http.Client{Timeout: 120 * time.Second},
	}
}

// WarmDir scans dir recursively and indexes every file by basename.
func (s *Server) WarmDir(dir string) {
	count := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		s.mu.Lock()
		s.cache[strings.ToLower(info.Name())] = path
		s.mu.Unlock()
		count++
		return nil
	})
	log.Printf("[cdn] indexed %d files under %s", count, dir)
}

func (s *Server) resolve(name string) (string, bool) {
	s.mu.RLock()
	p, ok := s.cache[strings.ToLower(name)]
	s.mu.RUnlock()
	return p, ok
}

// inflightFor serializes concurrent origin fetches of the same file.
func (s *Server) inflightFor(name string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.inflight[name]
	if !ok {
		m = &sync.Mutex{}
		s.inflight[name] = m
	}
	return m
}

// fetchOrigin downloads name from the origin CDN into the cdn root and
// returns the local path.
func (s *Server) fetchOrigin(name string) (string, error) {
	if s.OriginBase == "" {
		return "", fmt.Errorf("origin disabled")
	}
	m := s.inflightFor(name)
	m.Lock()
	defer m.Unlock()

	if p, ok := s.resolve(name); ok {
		return p, nil // served by a concurrent fetch
	}
	dest := filepath.Join(s.Root, name)
	tmp := dest + ".part"
	resp, err := s.client.Get(s.OriginBase + "/" + name)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("origin http %d", resp.StatusCode)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if err == nil {
			err = closeErr
		}
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.cache[strings.ToLower(name)] = dest
	s.mu.Unlock()
	return dest, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(strings.SplitN(r.URL.Path, "?", 2)[0])
		path, ok := s.resolve(name)
		if !ok {
			log.Printf("[cdn] MISS %s (origin fetch)", r.URL.Path)
			var err error
			path, err = s.fetchOrigin(name)
			if err != nil {
				log.Printf("[cdn] ORIGIN-FAIL %s: %v", r.URL.Path, err)
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			log.Printf("[cdn] CACHED %s", name)
		}
		f, err := os.Open(path)
		if err != nil {
			http.Error(w, "open failed", http.StatusInternalServerError)
			return
		}
		defer func() { _ = f.Close() }()
		st, _ := f.Stat()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", st.Size()))
		w.Header().Set("Accept-Ranges", "bytes")
		_, _ = io.Copy(w, f)
	})
	return mux
}
