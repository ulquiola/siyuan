// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !android && !ios

package tailscale

import (
	"context"
	"sync"
	"time"

	"github.com/siyuan-note/logging"
	"tailscale.com/tsnet"
)

// stateCache 保存最近一次节点状态，供拨号快速失败和设置页轮询共用。
type stateCache struct {
	mu           sync.Mutex
	status       NodeStatus
	ready        bool
	pendingRetry bool
	keyExpiry    string
	onRunning    []func()
}

// resetStatus 在节点重建时清空状态，保留回调、待补同步标记和最近一次密钥到期时间。
func (c *stateCache) resetStatus() {
	c.mu.Lock()
	c.status = NodeStatus{}
	c.ready = false
	c.mu.Unlock()
}

// update 写入状态并返回是否首次进入 running；首次进入时在解锁后执行回调。
func (c *stateCache) update(status NodeStatus) bool {
	c.mu.Lock()
	if "" != status.KeyExpiry {
		c.keyExpiry = status.KeyExpiry
	} else if "" != c.keyExpiry {
		// 进入 NeedsLogin 后 tailscale 不再返回 netmap 和 KeyExpiry，用最近一次值判断是否过期
		status.KeyExpiry = c.keyExpiry
	}
	wasRunning := c.ready && "running" == c.status.State
	c.status = status
	c.ready = true
	becameRunning := !wasRunning && "running" == status.State
	callbacks := append([]func(){}, c.onRunning...)
	c.mu.Unlock()

	if becameRunning {
		for _, callback := range callbacks {
			callback()
		}
	}
	return becameRunning
}

// withKeyExpiry 给缺少 KeyExpiry 的实时状态补上最近一次缓存值。
func (c *stateCache) withKeyExpiry(status NodeStatus) NodeStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if "" == status.KeyExpiry && "" != c.keyExpiry {
		status.KeyExpiry = c.keyExpiry
	}
	return status
}

func (c *stateCache) snapshot() NodeStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// isBlocked 表示节点需要用户登录或管理员批准，此时同步应快速失败并等待恢复。
func (c *stateCache) isBlocked() bool {
	state := c.snapshot().State
	return "needsLogin" == state || "needsMachineAuth" == state
}

func (c *stateCache) setPendingRetry() {
	c.mu.Lock()
	c.pendingRetry = true
	c.mu.Unlock()
}

func (c *stateCache) consumePendingRetry() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	ret := c.pendingRetry
	c.pendingRetry = false
	return ret
}

func (c *stateCache) setOnRunning(callback func()) {
	c.mu.Lock()
	c.onRunning = []func(){callback}
	c.mu.Unlock()
}

var nodeState stateCache

// SetOnRunning 注册节点首次进入 running 时的回调，重复注册会替换旧回调。
func SetOnRunning(callback func()) {
	nodeState.setOnRunning(callback)
}

// IsBlocked 返回节点是否处于需要登录或管理员批准的状态。
func IsBlocked() bool {
	return nodeState.isBlocked()
}

// SetPendingRetry 标记有同步因节点未就绪而失败，节点恢复 running 后应补一次同步。
func SetPendingRetry() {
	nodeState.setPendingRetry()
}

// ConsumePendingRetry 读取并清除待补同步标记。
func ConsumePendingRetry() bool {
	return nodeState.consumePendingRetry()
}

// SyncBlockedMessage 返回同步被节点状态阻断时给用户看的提示，语言文件缺失时回退英文。
func SyncBlockedMessage() string {
	if "needsMachineAuth" == nodeState.snapshot().State {
		if text := localized("tailscaleNeedsMachineAuth"); "" != text {
			return text
		}
		return "Embedded Tailscale is waiting for machine approval"
	}
	if text := localized("tailscaleSyncBlocked"); "" != text {
		return text
	}
	return "Embedded Tailscale is not logged in; sync resumes after login"
}

// watchNode 轮询当前节点状态并写入缓存，节点被替换后退出。
func watchNode(server *tsnet.Server) {
	defer logging.Recover()
	for {
		if !isCurrentNode(server) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), statusQueryTimeout)
		client, err := server.LocalClient()
		if nil == err {
			if status, queryErr := client.StatusWithoutPeers(ctx); nil == queryErr && isCurrentNode(server) {
				nodeState.update(nodeState.withKeyExpiry(statusFromState(status)))
			}
		}
		cancel()

		delay := 2 * time.Second
		if "running" == nodeState.snapshot().State {
			delay = 10 * time.Second
		}
		time.Sleep(delay)
	}
}
