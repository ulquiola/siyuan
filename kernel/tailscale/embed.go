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

//go:build !android && !ios

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/socks5"
	"tailscale.com/net/tshttpproxy"
	"tailscale.com/tsnet"
)

const (
	nodeOnlineLogInterval = time.Second
	statusQueryTimeout    = 5 * time.Second
	// controlErrLimit 限制交给界面的控制面错误长度，日志里的完整信息仍保留在内核日志中
	controlErrLimit = 200
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
	server      *tsnet.Server
	listener    net.Listener
	config      *Config
	closeServer func() error
}

func (n *runningNode) stop() {
	n.listener.Close()
	closeServer := n.closeServer
	if nil == closeServer {
		closeServer = n.server.Close
	}
	if err := closeServer(); nil != err && !errors.Is(err, net.ErrClosed) {
		logging.LogWarnf("tailscale: stop node [%s] failed: %s", n.config.Hostname, err)
	}
}

var (
	// applyMu 串行化节点的启停，避免同一状态目录被两个节点同时占用
	applyMu sync.Mutex
	nodeMu  sync.Mutex
	current *runningNode
	lastErr string

	// 状态查询可能超时或失败，登录链接单独缓存，保证失败时仍能交给用户完成登录
	authURLMu   sync.Mutex
	lastAuthURL string

	// 控制面错误只出现在 tsnet 内部日志里，单独留下最后一条供设置界面展示
	controlErrMu sync.Mutex
	controlErr   string
	// controlLogLast 折叠连续重复的控制面日志，避免轮询期间反复写日志
	controlLogMu   sync.Mutex
	controlLogLast string

	controlProxyOnce sync.Once
)

// Apply 按配置文件重建内嵌节点：配置缺失或未启用时停止节点，否则先停再启。
//
// 本函数会等待旧节点关闭，因此由调用方放到后台执行；失败只记录状态和日志，不影响内核启动。
func Apply(configDir string) {
	defer logging.Recover()
	ensureControlProxyDirect()

	applyMu.Lock()
	defer applyMu.Unlock()

	nodeMu.Lock()
	previous := current
	current = nil
	lastErr = ""
	nodeMu.Unlock()
	// 清空发生在重建开始时、旧节点停止之前，此后缓存的链接只会来自新节点，
	// 禁用或启动失败时也不会沿用上一节点残留的链接
	rememberAuthURL("")
	resetControlError()
	if nil != previous {
		previous.stop()
	}

	config, err := LoadConfig(configDir)
	if nil != err {
		setLastError("tailscaleConfigError", err)
		return
	}
	if nil == config || !config.IsEnabled() {
		return
	}

	listener, err := net.Listen("tcp", config.Listen)
	if nil != err {
		setLastError("tailscaleStartError", fmt.Errorf("listen [%s] failed: %w", config.Listen, err))
		return
	}

	server := &tsnet.Server{
		Dir:      config.StateDir,
		Hostname: config.Hostname,
		AuthKey:  config.AuthKey,
		Logf:     controlLogf,
		UserLogf: (&userLogf{}).logf,
	}
	if err = server.Start(); nil != err {
		listener.Close()
		setLastError("tailscaleStartError", fmt.Errorf("start node [%s] failed: %w", config.Hostname, err))
		return
	}

	nodeMu.Lock()
	current = &runningNode{server: server, listener: listener, config: config, closeServer: server.Close}
	nodeMu.Unlock()
	logging.LogInfof("tailscale: serving SOCKS5 on [%s] for node [%s]", config.Listen, config.Hostname)

	go func() {
		defer logging.Recover()
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
			return NodeStatus{State: "error", Error: errMessage, AuthURL: cachedAuthURL()}
		}
		return NodeStatus{State: "disabled"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), statusQueryTimeout)
	defer cancel()
	client, err := node.server.LocalClient()
	if nil != err {
		return NodeStatus{State: "error", Error: err.Error(), AuthURL: cachedAuthURL()}
	}
	// 状态查询只为拿到后端状态与登录链接，不需要 peer 列表，避免大网络下查询超时
	status, err := client.StatusWithoutPeers(ctx)
	if nil != err {
		return NodeStatus{State: "error", Error: err.Error(), AuthURL: cachedAuthURL()}
	}
	ret := statusFromState(status)
	if "running" == ret.State && "" != lastControlError() {
		// 连接成功后清掉旧的控制面错误，避免节点再次断开时展示过期原因
		resetControlError()
	}
	// 只在查询成功时写入缓存，空串表示已完成登录，同样需要覆盖旧链接
	rememberAuthURL(ret.AuthURL)
	return withControlDetail(ret)
}

// StatusFor 返回内嵌节点当前的运行状态，并结合配置区分「未启用」与「已启用但节点还没建好」。
//
// 用户刚保存配置时节点需要重新建立，这期间状态查询只能得到未启用，此时按正在连接上报，避免界面显示成开关没有生效。
func StatusFor(config *Config) NodeStatus {
	status := Status()
	if nil != config && config.IsEnabled() && "disabled" == status.State {
		status.State = "starting"
	}
	return status
}

// rememberAuthURL 缓存最近一次成功查询得到的登录链接，空串即为清空。
func rememberAuthURL(url string) {
	authURLMu.Lock()
	lastAuthURL = url
	authURLMu.Unlock()
}

// cachedAuthURL 返回缓存的登录链接，供状态查询失败时继续展示。
func cachedAuthURL() string {
	authURLMu.Lock()
	defer authURLMu.Unlock()
	return lastAuthURL
}

func statusFromState(status *ipnstate.Status) NodeStatus {
	ret := NodeStatus{State: "starting", AuthURL: status.AuthURL}
	switch status.BackendState {
	case "Running":
		ret.State = "running"
	case "NeedsLogin", "NeedsMachineAuth":
		ret.State = "needsLogin"
	// 未启动的节点仍可能带着登录链接，按未就绪展示，避免把用户的登录提示当成错误
	case "Starting", "NoState", "Stopped":
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

func setLastError(key string, err error) {
	message := err.Error()
	if prefix := localized(key); "" != prefix {
		message = prefix + ": " + message
	}
	nodeMu.Lock()
	lastErr = message
	nodeMu.Unlock()
	logging.LogWarnf("tailscale: %s", message)
}

// localized 返回当前界面语言下的提示文案，语言文件缺失时回退到英文。
func localized(key string) string {
	if text := util.I18nTerm(util.Lang, key); "" != text {
		return text
	}
	switch key {
	case "tailscaleConfigError":
		return "Tailscale configuration error"
	case "tailscaleStartError":
		return "Tailscale failed to start"
	}
	return ""
}

// ensureControlProxyDirect 让 Tailscale 自身的网络请求始终直连。
//
// 控制面连接不能跟随思源配置的网络代理：Go 会缓存首次读取的代理环境变量，运行中改代理不会生效；
// 代理指向本项目自己的 SOCKS 监听地址时还会形成控制面等待节点、节点等待控制面的死锁。
func ensureControlProxyDirect() {
	controlProxyOnce.Do(func() {
		if err := tshttpproxy.SetProxyFunc(func(*url.URL) (*url.URL, error) { return nil, nil }); nil != err {
			logging.LogWarnf("tailscale: set direct control proxy failed: %s", err)
		}
	})
}

// controlLogf 把 tsnet 的控制面日志转写到内核日志，并留下最后一条错误供设置界面展示。
//
// 控制面失败只写进 tsnet 内部日志、不进状态查询结果，界面拿不到，用户只能翻 tailscaled.log。
func controlLogf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if !strings.Contains(message, "control:") {
		return
	}
	controlLogMu.Lock()
	changed := message != controlLogLast
	controlLogLast = message
	controlLogMu.Unlock()
	if !changed {
		return
	}

	if !strings.Contains(message, "failed") && !strings.Contains(message, "error") {
		logging.LogInfof("tailscale: %s", message)
		return
	}
	// 去掉前缀，只保留能直接展示给用户的部分；按字符截断，避免切坏多字节文本
	detail := strings.TrimSpace(strings.TrimPrefix(message, "control: "))
	if runes := []rune(detail); controlErrLimit < len(runes) {
		detail = string(runes[:controlErrLimit])
	}
	controlErrMu.Lock()
	controlErr = detail
	controlErrMu.Unlock()
	logging.LogWarnf("tailscale: %s", message)
}

// withControlDetail 在未连接状态下补上最近一次控制面错误，已连接时保持为空。
func withControlDetail(status NodeStatus) NodeStatus {
	if "running" == status.State || "" != status.Error {
		return status
	}
	status.Error = lastControlError()
	return status
}

// lastControlError 返回最近一次记录的控制面错误。
func lastControlError() string {
	controlErrMu.Lock()
	defer controlErrMu.Unlock()
	return controlErr
}

func resetControlError() {
	controlErrMu.Lock()
	controlErr = ""
	controlErrMu.Unlock()

	controlLogMu.Lock()
	controlLogLast = ""
	controlLogMu.Unlock()
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
		if !isCurrentNode(server) {
			return
		}
		if ip4, _ := server.TailscaleIPs(); ip4.IsValid() {
			logging.LogInfof("tailscale: node [%s] online, tailnet IP [%s]", hostname, ip4)
			return
		}
		time.Sleep(nodeOnlineLogInterval)
	}
	if isCurrentNode(server) {
		logging.LogWarnf("tailscale: node [%s] has no tailnet IP after %s, check login or network", hostname, 120*nodeOnlineLogInterval)
	}
}

// isCurrentNode 判断节点是否仍是当前节点，避免旧节点的后台任务继续轮询或告警。
func isCurrentNode(server *tsnet.Server) bool {
	nodeMu.Lock()
	defer nodeMu.Unlock()
	return nil != current && current.server == server
}
