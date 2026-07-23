package imap

import (
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
	listeners []net.Listener
	wg        sync.WaitGroup
	shutdown  chan struct{}
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
	}
}

// Ports defines the ports for IMAP services.
type Ports struct {
	IMAP    int // 143 (STARTTLS)
	IMAPTLS int // 993 (implicit TLS)
}

// ListenAndServe starts IMAP listeners on the specified ports. A zero port is
// skipped. It returns once the listeners are open; connections are served in the
// background until [Server.Shutdown].
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

	s.listeners = append(s.listeners, listener)

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

		go func() {
			defer s.limiter.Release(ip)
			session := NewSession(conn, s.backend, s.hostname, s.tlsConfig, s.limiter)
			if implicitTLS {
				session.usingTLS = true
			}
			session.Handle()
		}()
	}
}

// Shutdown gracefully stops the server: it closes all listeners and waits for
// in-flight sessions to finish.
func (s *Server) Shutdown() {
	close(s.shutdown)
	for _, l := range s.listeners {
		_ = l.Close()
	}
	s.wg.Wait()
	slog.Info("imap: server stopped")
}

// extractIP extracts the IP address from a host:port string.
func extractIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
