// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build android || ios

package tailscale

import (
	"context"
	"net"

	"github.com/siyuan-note/httpclient"
)

// DialContext 在移动端不可用：移动端由系统级 Tailscale 客户端处理网络。
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return nil, ErrDisabled
}

// NewSyncTransport 在移动端保留原有网络代理行为，内嵌节点只存在于桌面端。
func NewSyncTransport(skipTLSVerify bool) *SyncTransport {
	return &SyncTransport{Transport: httpclient.NewTransport(skipTLSVerify), mode: SyncNetworkDirect}
}
