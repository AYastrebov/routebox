package webfront

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ErrAlreadyRunning is returned by Start while the front is listening.
var ErrAlreadyRunning = errors.New("web front is already running")

// shutdownTimeout bounds how long Stop waits for in-flight requests.
const shutdownTimeout = 15 * time.Second

// Front is the loopback HTTP server. Its handler is swapped atomically, so the
// Telegram WEB proxy can come and go without touching the listener. All
// methods are safe for concurrent use.
type Front struct {
	stub http.Handler
	web  atomic.Pointer[http.Handler]

	mu     sync.Mutex // guards the running listener and its address
	run    *running
	listen string
}

// running pairs a server with its listener. Shutdown only closes listeners
// Serve has already registered, so a stop right after a start would leak the
// port; closing the listener ourselves makes that race harmless.
type running struct {
	srv *http.Server
	l   net.Listener
}

// New returns a stopped front that serves stub until a web handler is set.
func New(stub http.Handler) *Front {
	return &Front{stub: stub}
}

// SetWebHandler makes h answer every request; nil reverts to the stub.
func (f *Front) SetWebHandler(h http.Handler) {
	if h == nil {
		f.web.Store(nil)

		return
	}

	f.web.Store(&h)
}

// ServeHTTP dispatches to the current handler. It is read per request so a
// swap takes effect immediately, without restarting the server.
func (f *Front) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := f.web.Load(); h != nil {
		(*h).ServeHTTP(w, r)

		return
	}

	f.stub.ServeHTTP(w, r)
}

// Listen returns the bound address, or "" when not running.
func (f *Front) Listen() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.listen
}

// Start binds listen and serves in the background.
func (f *Front) Start(listen string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.run != nil {
		return ErrAlreadyRunning
	}

	run, addr, err := f.bind(listen)
	if err != nil {
		return err
	}

	f.run, f.listen = run, addr

	return nil
}

// Rebind moves the front to a new address. The new listener is bound before
// the old one is closed, so on failure the old one keeps serving. A stopped
// front is started instead.
func (f *Front) Rebind(listen string) error {
	f.mu.Lock()

	run, addr, err := f.bind(listen)
	if err != nil {
		f.mu.Unlock()

		return err
	}

	old := f.run
	f.run, f.listen = run, addr
	f.mu.Unlock()

	return shutdown(old)
}

// Stop shuts the server down, waiting up to 15 s for in-flight requests and
// then closing. It is a no-op when not running.
func (f *Front) Stop() error {
	f.mu.Lock()
	old := f.run
	f.run, f.listen = nil, ""
	f.mu.Unlock()

	return shutdown(old)
}

// bind opens listen and starts a server on it. A fresh http.Server per bind:
// one cannot be reused after Shutdown.
func (f *Front) bind(listen string) (*running, string, error) {
	l, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, "", fmt.Errorf("listen %s: %w", listen, err)
	}

	srv := &http.Server{
		Handler:           f,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       75 * time.Second,
		MaxHeaderBytes:    16 << 10,
		// The front proxy forwards OPTIONS to the handler; don't answer "*".
		DisableGeneralOptionsHandler: true,
	}

	go func() { _ = srv.Serve(l) }()

	return &running{srv: srv, l: l}, l.Addr().String(), nil
}

func shutdown(r *running) error {
	if r == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	err := r.srv.Shutdown(ctx)
	if err != nil {
		err = r.srv.Close()
	}

	_ = r.l.Close() // already closed by Shutdown unless Serve had not started yet

	return err
}
