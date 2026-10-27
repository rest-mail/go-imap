package rfctest

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rest-mail/go-imap/pkg/imap/server"
)

// The public listener API takes explicit ports. Reserve an ephemeral port long
// enough to choose it, then give it to that API; a bind race is a test failure.
func implicitSecurityServer(t *testing.T, cfg *tls.Config) (*server.Server, *backend, string) {
	t.Helper()
	reservation, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	addr := reservation.Addr().(*net.TCPAddr)
	host := "127.0.0.1"
	if addr.IP.To4() == nil {
		host = "::1"
	}
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	b := &backend{}
	s := server.New("imap.test", b, cfg, nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.ListenAndServe(server.Ports{IMAPTLS: addr.Port}); err != nil {
		t.Fatal(err)
	}
	return s, b, net.JoinHostPort(host, strconv.Itoa(addr.Port))
}

func implicitSecurityRoots(t *testing.T, cfg *tls.Config) *x509.CertPool {
	t.Helper()
	cert, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return roots
}

// Host-configured TLS1.2 and TLS1.3 listeners carry IMAP only after a verified
// handshake. This proves these concrete configurations, not the entire rev2
// cryptographic profile or a production certificate/port configuration.
// checklist: IMAP-SECURITY-02
func TestImplicitTLSProtectsAuthentication(t *testing.T) {
	for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
		t.Run(tls.VersionName(version), func(t *testing.T) {
			cfg := serverTLS(t)
			cfg.MinVersion, cfg.MaxVersion = version, version
			s, b, addr := implicitSecurityServer(t, cfg)
			raw, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = raw.Close() })
			if err := raw.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			conn := tls.Client(raw, &tls.Config{RootCAs: implicitSecurityRoots(t, cfg), ServerName: "imap.test", MinVersion: version, MaxVersion: version})
			if err := conn.Handshake(); err != nil {
				t.Fatal(err)
			}
			if state := conn.ConnectionState(); state.Version != version || len(state.VerifiedChains) == 0 {
				t.Fatalf("unverified or unexpected TLS state: version=%x chains=%d", state.Version, len(state.VerifiedChains))
			}
			c := &client{t: t, conn: conn, r: bufio.NewReader(conn), backend: b}
			if greeting := c.line(); !strings.HasPrefix(greeting, "* OK ") {
				t.Fatalf("greeting: %q", greeting)
			}
			requireCaps(t, c.capabilities("C1"), []string{"IMAP4rev1", "AUTH=PLAIN"}, []string{"STARTTLS", "LOGINDISABLED", "IMAP4rev2"})
			if version == tls.VersionTLS12 {
				_, tagged := c.command("A1", "LOGIN %s %s", testUser, testPass)
				requireOK(t, tagged, "A1")
			} else {
				requireOK(t, c.authenticate("A1", "PLAIN", plainResponse(testUser, testPass)), "A1")
			}
			_ = raw.Close()
			_ = s.Close()
			if b.calls != 1 {
				t.Fatalf("backend calls=%d, want1", b.calls)
			}
		})
	}
}

// Failed identity verification or incompatible TLS versions never dispatch
// credentials to the backend. Client verification is exercised as a fixture;
// this server library does not implement an IMAP client's identity policy.
// checklist: IMAP-SECURITY-03
func TestImplicitTLSHandshakeRefusal(t *testing.T) {
	for _, kind := range []string{"wrong hostname", "unknown authority", "incompatible version"} {
		t.Run(kind, func(t *testing.T) {
			cfg := serverTLS(t)
			cfg.MinVersion = tls.VersionTLS13
			s, b, addr := implicitSecurityServer(t, cfg)
			clientCfg := &tls.Config{RootCAs: implicitSecurityRoots(t, cfg), ServerName: "imap.test", MinVersion: tls.VersionTLS13}
			switch kind {
			case "wrong hostname":
				clientCfg.ServerName = "wrong.example.test"
			case "unknown authority":
				clientCfg.RootCAs = x509.NewCertPool()
			case "incompatible version":
				clientCfg.MinVersion, clientCfg.MaxVersion = tls.VersionTLS12, tls.VersionTLS12
			}
			conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, clientCfg)
			if conn != nil {
				_ = conn.Close()
			}
			_ = s.Close()
			if err == nil || b.calls != 0 {
				t.Fatalf("failed TLS accepted or backend reached: error=%v calls=%d", err, b.calls)
			}
		})
	}
}
