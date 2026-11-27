// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package tailscale

import (
	"errors"
	"net/http"
)

var (
	ErrDisabled         = errors.New("embedded tailscale disabled")
	ErrNeedsLogin       = errors.New("embedded tailscale needs login")
	ErrNeedsMachineAuth = errors.New("embedded tailscale needs machine auth")
	ErrNotReady         = errors.New("embedded tailscale not ready")
)

// stateError 让调用方用 errors.Is 区分节点状态，同时向用户展示本地化文案。
type stateError struct {
	message string
	kind    error
}

func (e *stateError) Error() string { return e.message }

func (e *stateError) Unwrap() error { return e.kind }

// SyncTransport 是同步客户端使用的 HTTP 传输，mode 记录当前是直连还是经内嵌节点。
type SyncTransport struct {
	*http.Transport
	mode string
}

func (t *SyncTransport) Mode() string {
	return t.mode
}
