package rfctest

import (
	"net"
	"strings"
	"testing"

	"github.com/rest-mail/go-imap/pkg/imap/server"
)

// A port labelled implicit TLS must never turn into a plaintext listener when
// the caller forgets its TLS configuration. Reject the entire startup request
// before attempting even the optional cleartext listener. An occupied port
// also distinguishes configuration refusal from a later bind error.
// checklist: IMAP-SECURITY-01
func TestImplicitTLSMissingConfiguration(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	port := occupied.Addr().(*net.TCPAddr).Port
	for _, both := range []bool{false, true} {
		t.Run(map[bool]string{false: "implicit only", true: "both listeners"}[both], func(t *testing.T) {
			s := server.New("imap.test", &backend{}, nil, nil)
			t.Cleanup(func() { _ = s.Close() })
			ports := server.Ports{IMAPTLS: port}
			if both {
				ports.IMAP = port
			}
			err := s.ListenAndServe(ports)
			if err == nil || !strings.Contains(err.Error(), "implicit TLS requires a TLS configuration") {
				t.Fatalf("startup must reject missing TLS before attempting either occupied port: %v", err)
			}
		})
	}
}
