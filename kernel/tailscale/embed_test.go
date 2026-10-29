// SiYuan - From thought to insight, with agents
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

//go:build !mobile

package tailscale

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/tshttpproxy"
)

func TestConfigureSelfProxySkipsOwnSOCKSListener(t *testing.T) {
	t.Setenv("HTTP_PROXY", "socks5://127.0.0.1:1055")
	t.Setenv("HTTPS_PROXY", "socks5://127.0.0.1:1055")
	t.Setenv("NO_PROXY", "")

	configureSelfProxy("127.0.0.1:1055")

	request, err := http.NewRequest(http.MethodGet, "https://controlplane.tailscale.com/key", nil)
	if nil != err {
		t.Fatalf("create request: %s", err)
	}
	proxy, err := tshttpproxy.ProxyFromEnvironment(request)
	if nil != err {
		t.Fatalf("lookup proxy: %s", err)
	}
	if nil != proxy {
		t.Fatalf("expected direct connection, got proxy [%s]", proxy)
	}
}

func TestStatusFromState(t *testing.T) {
	running := statusFromState(&ipnstate.Status{
		BackendState: "Running",
		TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.5")},
	})
	if "running" != running.State || 1 != len(running.IPs) || "100.64.0.5" != running.IPs[0] {
		t.Fatalf("unexpected running status: %#v", running)
	}

	needsLogin := statusFromState(&ipnstate.Status{BackendState: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/1"})
	if "needsLogin" != needsLogin.State || "https://login.tailscale.com/a/1" != needsLogin.AuthURL {
		t.Fatalf("unexpected login status: %#v", needsLogin)
	}

	stopped := statusFromState(&ipnstate.Status{BackendState: "Stopped", Health: []string{"backend error"}})
	if "error" != stopped.State || "Stopped: backend error" != stopped.Error {
		t.Fatalf("unexpected stopped status: %#v", stopped)
	}

	starting := statusFromState(&ipnstate.Status{BackendState: "Starting"})
	if "starting" != starting.State {
		t.Fatalf("unexpected starting status: %#v", starting)
	}
}

func TestUserLogfFoldsRepeatedMessages(t *testing.T) {
	logger := &userLogf{}
	if !logger.shouldLog("login required") {
		t.Fatal("first message should be logged")
	}
	if logger.shouldLog("login required") {
		t.Fatal("repeated message should be folded")
	}
	if !logger.shouldLog("node online") {
		t.Fatal("changed message should be logged")
	}
	if !logger.shouldLog("login required") {
		t.Fatal("message after a different one should be logged again")
	}
}

func TestServeSOCKSForwardsThroughDialer(t *testing.T) {
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if nil != err {
		t.Fatalf("listen echo: %s", err)
	}
	defer echoListener.Close()
	go func() {
		for {
			conn, acceptErr := echoListener.Accept()
			if nil != acceptErr {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if nil != err {
		t.Fatalf("listen proxy: %s", err)
	}
	defer proxyListener.Close()
	// 拨号器忽略请求里的地址，直接转发到本地回显服务，用来验证 SOCKS5 与自定义拨号的接驳
	dialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", echoListener.Addr().String())
	}
	go serveSOCKS(proxyListener, dialer)

	conn := socks5Connect(t, proxyListener.Addr().String())
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); nil != err {
		t.Fatalf("set deadline: %s", err)
	}
	if _, err = conn.Write([]byte("ping")); nil != err {
		t.Fatalf("write payload: %s", err)
	}
	reply := make([]byte, 4)
	if _, err = io.ReadFull(conn, reply); nil != err {
		t.Fatalf("read payload: %s", err)
	}
	if "ping" != string(reply) {
		t.Fatalf("payload = %q, want %q", reply, "ping")
	}
}

func socks5Connect(t *testing.T, proxyAddr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", proxyAddr)
	if nil != err {
		t.Fatalf("dial proxy: %s", err)
	}
	if _, err = conn.Write([]byte{0x05, 0x01, 0x00}); nil != err {
		t.Fatalf("write greeting: %s", err)
	}
	greeting := make([]byte, 2)
	if _, err = io.ReadFull(conn, greeting); nil != err {
		t.Fatalf("read greeting: %s", err)
	}
	if 0x05 != greeting[0] || 0x00 != greeting[1] {
		t.Fatalf("greeting = %v, want [5 0]", greeting)
	}

	request := []byte{0x05, 0x01, 0x00, 0x03, 11}
	request = append(request, []byte("example.com")...)
	request = append(request, 0x00, 0x50)
	if _, err = conn.Write(request); nil != err {
		t.Fatalf("write connect request: %s", err)
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(conn, header); nil != err {
		t.Fatalf("read connect reply: %s", err)
	}
	if 0x00 != header[1] {
		t.Fatalf("connect reply code = %d, want 0", header[1])
	}
	switch header[3] {
	case 0x01:
		_, err = io.ReadFull(conn, make([]byte, 4+2))
	case 0x04:
		_, err = io.ReadFull(conn, make([]byte, 16+2))
	case 0x03:
		length := make([]byte, 1)
		if _, err = io.ReadFull(conn, length); nil == err {
			_, err = io.ReadFull(conn, make([]byte, int(length[0])+2))
		}
	default:
		t.Fatalf("unexpected address type %d", header[3])
	}
	if nil != err {
		t.Fatalf("read bound address: %s", err)
	}
	return conn
}
