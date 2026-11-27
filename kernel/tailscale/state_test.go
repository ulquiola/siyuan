// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !android && !ios

package tailscale

import (
	"strings"
	"testing"
)

func TestStateCacheFiresOnRunningOnce(t *testing.T) {
	var cache stateCache
	fired := 0
	cache.setOnRunning(func() { fired++ })
	if cache.update(NodeStatus{State: "starting"}) {
		t.Fatal("starting should not be a running transition")
	}
	if !cache.update(NodeStatus{State: "running"}) {
		t.Fatal("first running update should be a transition")
	}
	if cache.update(NodeStatus{State: "running"}) {
		t.Fatal("staying running should not be a transition")
	}
	if 1 != fired {
		t.Fatalf("callbacks = %d, want 1", fired)
	}
}

func TestStateCacheBlockedAndPendingRetry(t *testing.T) {
	var cache stateCache
	for _, state := range []string{"needsLogin", "needsMachineAuth"} {
		cache.update(NodeStatus{State: state})
		if !cache.isBlocked() {
			t.Fatalf("%s should be blocked", state)
		}
	}
	cache.update(NodeStatus{State: "running"})
	if cache.isBlocked() {
		t.Fatal("running should not be blocked")
	}
	cache.setPendingRetry()
	if !cache.consumePendingRetry() {
		t.Fatal("pending retry should be consumed once")
	}
	if cache.consumePendingRetry() {
		t.Fatal("pending retry should be cleared after consumption")
	}
}

func TestStateCacheKeepsKeyExpiry(t *testing.T) {
	var cache stateCache
	expiry := "2026-12-01T00:00:00Z"
	cache.update(NodeStatus{State: "running", KeyExpiry: expiry})
	// 进入 needsLogin 后实时状态不再携带 KeyExpiry，缓存值要继续供过期判断使用
	cache.update(NodeStatus{State: "needsLogin"})
	if got := cache.snapshot().KeyExpiry; expiry != got {
		t.Fatalf("cached expiry = %q, want %q", got, expiry)
	}
	if got := cache.withKeyExpiry(NodeStatus{State: "needsLogin"}).KeyExpiry; expiry != got {
		t.Fatalf("filled expiry = %q, want %q", got, expiry)
	}
}

func TestSyncBlockedMessage(t *testing.T) {
	if "" == SyncBlockedMessage() {
		t.Fatal("blocked message should not be empty even without language files")
	}
	nodeState.resetStatus()
	nodeState.update(NodeStatus{State: "needsMachineAuth"})
	t.Cleanup(func() { nodeState.resetStatus() })
	msg := SyncBlockedMessage()
	if !strings.Contains(msg, "approval") && !strings.Contains(msg, "批准") {
		t.Fatalf("machine auth message should mention approval, got %q", msg)
	}
}
