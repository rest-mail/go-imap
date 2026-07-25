package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// Server listens for IMAP connections and spawns a session handler per client.
type Server struct {
	hostname  string
	backend   Backend
	tlsConfig *tls.Config
	limiter   Limiter

	// shutdown is closed once, by the first Shutdown or Close, to tell the
	// accept loops to stop.
	shutdown chan struct{}

	// wg tracks every goroutine the server owns: each accept loop and each
	// in-flight session. Shutdown waits on it to drain active sessions.
	wg sync.WaitGroup

	// mu guards listeners, conns and closed. It also serializes the wg.Add for
	// a new session against the closed flag, so no session is launched (and
	// missed by Shutdown's wait) after shutdown has begun.
	mu        sync.Mutex
	listeners []net.Listener
	conns     map[net.Conn]struct{}
	closed    bool
}

// NewServer creates an IMAP Server backed by the given [Backend]. hostname is
// announced in the server greeting. A non-nil tlsConfig enables STARTTLS and
// implicit-TLS listeners. A nil limiter defaults to [NopLimiter].
func NewServer(hostname string, backend Backend, tlsConfig *tls.Config, limiter Limiter) *Server {
	if limiter == nil {
		limiter = NopLimiter{}
	}
	return &Server{
		hostname:  hostname,
		backend:   backend,
		tlsConfig: tlsConfig,
		limiter:   limiter,
		shutdown:  make(chan struct{}),
		conns:     make(map[net.Conn]struct{}),
	}
}

// Ports defines the ports for IMAP services.
type Ports struct {
	IMAP    int // 143 (STARTTLS)
	IMAPTLS int // 993 (implicit TLS)
}

// ListenAndServe starts IMAP listeners on the specified ports. A zero port is
// skipped. It returns once the listeners are open; connections are served in the
// background until [Server.Shutdown] or [Server.Close].
func (s *Server) ListenAndServe(ports Ports) error {
	if ports.IMAP > 0 {
		if err := s.listen(ports.IMAP, false); err != nil {
			return fmt.Errorf("failed to listen on port %d: %w", ports.IMAP, err)
		}
	}
	if ports.IMAPTLS > 0 {
		if err := s.listen(ports.IMAPTLS, true); err != nil {
			return fmt.Errorf("failed to listen on port %d: %w", ports.IMAPTLS, err)
		}
	}
	return nil
}

func (s *Server) listen(port int, implicitTLS bool) error {
	addr := fmt.Sprintf(":%d", port)
	var listener net.Listener
	var err error

	if implicitTLS && s.tlsConfig != nil {
		listener, err = tls.Listen("tcp", addr, s.tlsConfig)
		if err != nil {
			return err
		}
		slog.Info("imap: listening (implicit TLS)", "port", port)
	} else {
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		slog.Info("imap: listening", "port", port)
	}

	s.mu.Lock()
	s.listeners = append(s.listeners, listener)
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(listener, implicitTLS)
	}()

	return nil
}

func (s *Server) acceptLoop(listener net.Listener, implicitTLS bool) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.shutdown:
				return
			default:
				slog.Error("imap: accept error", "error", err)
				continue
			}
		}

		ip := extractIP(conn.RemoteAddr().String())
		if !s.limiter.Accept(ip) {
			slog.Warn("imap: connection rejected by limiter", "ip", ip)
			_ = conn.Close()
			continue
		}

		// Register the connection (and its wg slot) before serving it. If the
		// server is already shutting down, refuse the connection and stop
		// accepting — the listener is being torn down anyway.
		if !s.trackConn(conn) {
			s.limiter.Release(ip)
			_ = conn.Close()
			return
		}

		go func() {
			defer s.wg.Done()
			defer s.untrackConn(conn)
			defer s.limiter.Release(ip)
			session := NewSession(conn, s.backend, s.hostname, s.tlsConfig, s.limiter)
			if implicitTLS {
				session.usingTLS = true
			}
			session.Handle()
		}()
	}
}

// trackConn registers conn as in-flight and reserves its slot in the wait
// group, unless the server has begun shutting down (in which case it returns
// false and the caller must not serve the connection). Reserving the wg slot
// under the same lock as the closed flag guarantees the Add happens-before the
// closed=true that gates [Server.Shutdown]'s wait, so no session is ever missed.
func (s *Server) trackConn(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
	return true
}

// untrackConn removes conn from the in-flight set once its session has ended.
func (s *Server) untrackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// beginShutdown marks the server closed and stops it accepting new connections:
// it closes the shutdown channel and every listener. It is idempotent, so
// Shutdown and Close may be called in any order, concurrently, or repeatedly
// without panicking on a double close.
func (s *Server) beginShutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.shutdown)
	for _, l := range s.listeners {
		_ = l.Close()
	}
}

// Shutdown gracefully stops the server: it stops accepting new connections and
// then blocks until every in-flight session has finished, or until ctx is done.
// It returns nil once all sessions have drained, or ctx.Err() if the deadline
// passes first (the sessions keep running; call [Server.Close] to force them to
// stop). Shutdown mirrors the semantics of [net/http.Server.Shutdown].
func (s *Server) Shutdown(ctx context.Context) error {
	s.beginShutdown()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("imap: server stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close immediately stops the server: it stops accepting new connections and
// force-closes every active connection, aborting any in-flight sessions without
// waiting for them to drain. Unlike [Server.Shutdown] it does not block on the
// sessions; use Shutdown for a graceful stop. Close mirrors the semantics of
// [net/http.Server.Close] and always returns nil.
func (s *Server) Close() error {
	s.beginShutdown()

	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()

	slog.Info("imap: server closed")
	return nil
}

// extractIP extracts the IP address from a host:port string.
func extractIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
