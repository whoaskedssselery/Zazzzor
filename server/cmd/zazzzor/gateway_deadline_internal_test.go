package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestRejectedGatewayRequestKeepsBodyDeadline(t *testing.T) {
	const deadline = 200 * time.Millisecond
	h, _ := newModulesRouter(t, httpx.DeadlineOptions{Read: deadline, Write: 3 * time.Second})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("upgrade=%t", upgrade), func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatalf("dial gateway: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			if deadlineErr := conn.SetDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
				t.Fatalf("bound gateway client: %v", deadlineErr)
			}
			headers := ""
			if upgrade {
				headers = "Connection: Upgrade\r\nUpgrade: websocket\r\n"
			}
			// No session: the gateway must reject this as HTTP, without giving
			// its unfinished request body an unlimited drain deadline.
			_, err = fmt.Fprintf(conn, "GET /api/v1/gateway HTTP/1.1\r\nHost: test\r\nOrigin: %s\r\nContent-Length: 64\r\n%s\r\nx", testkit.DefaultOrigin, headers)
			if err != nil {
				t.Fatalf("send partial gateway request: %v", err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("rejected gateway did not respond after its body deadline: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("rejected gateway status = %d, want 401", resp.StatusCode)
			}
		})
	}
}
