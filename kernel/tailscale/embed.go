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
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/siyuan-note/logging"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/socks5"
	"tailscale.com/net/tshttpproxy"
	"tailscale.com/tsnet"
)

const (
	nodeOnlineLogInterval = time.Second
	statusQueryTimeout    = 2 * time.Second
)

// userLogf 把 tsnet 的用户日志转写到内核日志，并折叠连续重复的内容。
//
// 未登录时 tsnet 每 5 秒重复打印一次登录链接，直接转写会让内核日志持续膨胀。
type userLogf struct {
	mu   sync.Mutex
	last string
}

func (l *userLogf) logf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if !l.shouldLog(message) {
		return
	}
	logging.LogInfof("tailscale: %s", message)
}

func (l *userLogf) shouldLog(message string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if message == l.last {
		return false
	}
	l.last = message
	return true
}

type runningNode struct {
	server   *tsnet.Server
	listener net.Listener
	config   *Config
}

func (n *runningNode) stop() {
	n.listener.Close()
	if err := n.server.Close(); nil != err && !errors.Is(err, net.ErrClosed) {
		logging.LogWarnf("tailscale: stop node [%s] failed: %s", n.config.Hostname, err)
	}
}

var (
	// applyMu 串行化节点的启停，避免同一状态目录被两个节点同时占用
	applyMu sync.Mutex
	nodeMu  sync.Mutex
	current *runningNode
	lastErr string
)

// Apply 按配置文件重建内嵌节点：配置缺失或未启用时停止节点，否则先停再启。
//
// 本函数会等待旧节点关闭，因此由调用方放到后台执行；失败只记录状态和日志，不影响内核启动。
func Apply(configDir string) {
	applyMu.Lock()
	defer applyMu.Unlock()

	nodeMu.Lock()
	previous := current
	current = nil
	lastErr = ""
	nodeMu.Unlock()
	if nil != previous {
		previous.stop()
	}

	config, err := LoadConfig(configDir)
	if nil != err {
		setLastError(err)
		return
	}
	if nil == config || !config.IsEnabled() {
		return
	}

	// 控制面请求不能走本项目自己提供的 SOCKS 端口，否则节点上线前会形成自环
	configureSelfProxy(config.Listen)

	listener, err := net.Listen("tcp", config.Listen)
	if nil != err {
		setLastError(fmt.Errorf("listen [%s] failed: %w", config.Listen, err))
		return
	}

	server := &tsnet.Server{
		Dir:      config.StateDir,
		Hostname: config.Hostname,
		AuthKey:  config.AuthKey,
		UserLogf: (&userLogf{}).logf,
	}
	if err = server.Start(); nil != err {
		listener.Close()
		setLastError(fmt.Errorf("start node [%s] failed: %w", config.Hostname, err))
		return
	}

	nodeMu.Lock()
	current = &runningNode{server: server, listener: listener, config: config}
	nodeMu.Unlock()
	logging.LogInfof("tailscale: serving SOCKS5 on [%s] for node [%s]", config.Listen, config.Hostname)

	go func() {
		if serveErr := serveSOCKS(listener, server.Dial); nil != serveErr && !errors.Is(serveErr, net.ErrClosed) {
			logging.LogWarnf("tailscale: SOCKS5 service stopped: %s", serveErr)
		}
	}()
	go logNodeAddress(server, config.Hostname)
}

// Status 返回内嵌节点当前的运行状态。
func Status() NodeStatus {
	nodeMu.Lock()
	node := current
	errMessage := lastErr
	nodeMu.Unlock()

	if nil == node {
		if "" != errMessage {
			return NodeStatus{State: "error", Error: errMessage}
		}
		return NodeStatus{State: "disabled"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), statusQueryTimeout)
	defer cancel()
	client, err := node.server.LocalClient()
	if nil != err {
		return NodeStatus{State: "error", Error: err.Error()}
	}
	status, err := client.Status(ctx)
	if nil != err {
		return NodeStatus{State: "error", Error: err.Error()}
	}
	return statusFromState(status)
}

func statusFromState(status *ipnstate.Status) NodeStatus {
	ret := NodeStatus{State: "starting", AuthURL: status.AuthURL}
	switch status.BackendState {
	case "Running":
		ret.State = "running"
	case "NeedsLogin", "NeedsMachineAuth":
		ret.State = "needsLogin"
	case "Starting", "NoState":
		ret.State = "starting"
	default:
		ret.State = "error"
		ret.Error = status.BackendState
		if health := strings.Join(status.Health, "; "); "" != health {
			ret.Error += ": " + health
		}
	}
	for _, ip := range status.TailscaleIPs {
		ret.IPs = append(ret.IPs, ip.String())
	}
	return ret
}

func setLastError(err error) {
	nodeMu.Lock()
	lastErr = err.Error()
	nodeMu.Unlock()
	logging.LogWarnf("tailscale: %s", err)
}

// configureSelfProxy 让 Tailscale 自身的控制面连接绕过思源配置的代理。
//
// 当代理地址等于本项目的 SOCKS 监听地址时，控制请求会等待节点上线，而节点上线又依赖控制请求，
// 形成死锁；SetSelfProxy 会在 Tailscale 内部的代理配置中清除这个自指代理。
func configureSelfProxy(listen string) {
	tshttpproxy.SetSelfProxy(listen,
		"http://"+listen, "https://"+listen, "socks5://"+listen, "socks5h://"+listen)
}

func serveSOCKS(listener net.Listener, dial func(ctx context.Context, network, address string) (net.Conn, error)) error {
	return (&socks5.Server{
		Dialer: dial,
		Logf: func(format string, args ...any) {
			logging.LogInfof("tailscale: "+format, args...)
		},
	}).Serve(listener)
}

// logNodeAddress 在节点取得 Tailnet 地址后记录一次，便于确认真正加入了网络。
func logNodeAddress(server *tsnet.Server, hostname string) {
	defer logging.Recover()
	for range 120 {
		if ip4, _ := server.TailscaleIPs(); ip4.IsValid() {
			logging.LogInfof("tailscale: node [%s] online, tailnet IP [%s]", hostname, ip4)
			return
		}
		time.Sleep(nodeOnlineLogInterval)
	}
	logging.LogWarnf("tailscale: node [%s] has no tailnet IP after %s, check login or network", hostname, 120*nodeOnlineLogInterval)
}
