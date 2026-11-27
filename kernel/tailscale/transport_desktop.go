// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !android && !ios

package tailscale

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/siyuan-note/httpclient"
)

// nodeEnabled 记录配置是否启用内嵌节点；与 current 分开，便于在节点尚未建好时区分「未启用」和「正在连接」。
var nodeEnabled atomic.Bool

// noStateRetryDelay 是 tsnet 刚启动时 backend 仍为 NoState 的重试间隔，测试可替换。
var noStateRetryDelay = 200 * time.Millisecond

// readyWaitTimeout 限制等待节点进入 running 的时间，避免节点未就绪时把 S3/WebDAV 的整个超时耗光。
var readyWaitTimeout = 5 * time.Second

// readyPollInterval 是等待 running 时读取状态缓存的间隔，测试可替换。
var readyPollInterval = 100 * time.Millisecond

// dialNode 是 tsnet 拨号的测试替身入口。
var dialNode = func(node *runningNode, ctx context.Context, network, address string) (net.Conn, error) {
	return node.server.Dial(ctx, network, address)
}

// waitForRunningState 等到节点 running、进入需要用户处理的状态，或等待超时。
func waitForRunningState(ctx context.Context) NodeStatus {
	deadline := time.Now().Add(readyWaitTimeout)
	for {
		status := nodeState.snapshot()
		switch status.State {
		case "running", "needsLogin", "needsMachineAuth", "error":
			return status
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return status
		}
		select {
		case <-ctx.Done():
			return nodeState.snapshot()
		case <-time.After(readyPollInterval):
		}
	}
}

// DialContext 经当前内嵌节点拨号；节点未登录/待批准时立即返回可读错误，节点正在启动时只等待 readyWaitTimeout。
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	nodeMu.Lock()
	node := current
	nodeMu.Unlock()

	if nil == node {
		nodeState.setPendingRetry()
		if !nodeEnabled.Load() {
			return nil, &stateError{message: localizedError("tailscaleDisabled"), kind: ErrDisabled}
		}
		return nil, &stateError{message: localizedError("tailscaleStarting"), kind: ErrNotReady}
	}

	status := waitForRunningState(ctx)
	switch status.State {
	case "running":
		// 继续拨号
	case "needsLogin":
		nodeState.setPendingRetry()
		return nil, &stateError{message: localizedError("tailscaleNeedsLogin"), kind: ErrNeedsLogin}
	case "needsMachineAuth":
		nodeState.setPendingRetry()
		return nil, &stateError{message: localizedError("tailscaleNeedsMachineAuth"), kind: ErrNeedsMachineAuth}
	case "error":
		nodeState.setPendingRetry()
		message := status.Error
		if "" == message {
			message = localizedError("tailscaleStarting")
		}
		return nil, &stateError{message: message, kind: ErrNotReady}
	default:
		nodeState.setPendingRetry()
		return nil, &stateError{message: localizedError("tailscaleStarting"), kind: ErrNotReady}
	}

	for attempt := 0; ; attempt++ {
		conn, err := dialNode(node, ctx, network, address)
		if nil == err {
			return conn, nil
		}
		if ctx.Err() != nil {
			nodeState.setPendingRetry()
			return nil, err
		}
		if !isNoStateError(err) {
			return nil, err
		}
		if attempt < 2 {
			time.Sleep(noStateRetryDelay)
			continue
		}
		nodeState.setPendingRetry()
		return nil, &stateError{message: localizedError("tailscaleStarting"), kind: ErrNotReady}
	}
}

// NewSyncTransport 创建 S3/WebDAV 使用的传输：proxy 模式经内嵌节点拨号并忽略 HTTP 代理；
// direct 模式保留真实/系统代理，指向内嵌监听的代理已由内核在应用全局代理时忽略。
func NewSyncTransport(skipTLSVerify bool) *SyncTransport {
	base := httpclient.NewTransport(skipTLSVerify)
	mode := SyncNetworkMode()
	if SyncNetworkProxy == mode {
		base.Proxy = nil
		base.DialContext = DialContext
	}
	return &SyncTransport{Transport: base, mode: mode}
}

func localizedError(key string) string {
	if text := localized(key); "" != text {
		return text
	}
	switch key {
	case "tailscaleNeedsLogin":
		return "Embedded Tailscale needs login"
	case "tailscaleNeedsMachineAuth":
		return "Embedded Tailscale is waiting for machine approval"
	case "tailscaleDisabled":
		return "Embedded Tailscale is disabled"
	case "tailscaleStarting":
		return "Embedded Tailscale is starting"
	}
	return key
}

// isNoStateError 识别 tsnet 在刚启动时返回的终止状态错误；该状态稍后会自行进入 Starting/Running。
func isNoStateError(err error) bool {
	return strings.Contains(err.Error(), "backend in state NoState")
}
