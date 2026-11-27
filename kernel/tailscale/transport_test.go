// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !android && !ios

package tailscale

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestDialContextFastFailsWhenBlocked(t *testing.T) {
	oldNode, oldEnabled, oldDial, oldDelay := current, nodeEnabled.Load(), dialNode, noStateRetryDelay
	t.Cleanup(func() {
		current = oldNode
		nodeEnabled.Store(oldEnabled)
		dialNode = oldDial
		noStateRetryDelay = oldDelay
		nodeState.resetStatus()
	})

	for _, test := range []struct {
		state NodeStatus
		want  error
	}{{NodeStatus{State: "needsLogin"}, ErrNeedsLogin}, {NodeStatus{State: "needsMachineAuth"}, ErrNeedsMachineAuth}} {
		nodeState.resetStatus()
		nodeState.update(test.state)
		nodeEnabled.Store(true)
		current = &runningNode{}
		called := false
		dialNode = func(*runningNode, context.Context, string, string) (net.Conn, error) {
			called = true
			return nil, errors.New("should not dial")
		}
		_, err := DialContext(context.Background(), "tcp", "100.94.63.128:9000")
		if !errors.Is(err, test.want) {
			t.Fatalf("error = %v, want %v", err, test.want)
		}
		if called {
			t.Fatal("blocked state must not start a tsnet dial")
		}
		if !nodeState.consumePendingRetry() {
			t.Fatal("blocked dial should request a retry")
		}
	}
}

func TestDialContextDisabledAndNotReady(t *testing.T) {
	oldNode, oldEnabled, oldDial := current, nodeEnabled.Load(), dialNode
	t.Cleanup(func() {
		current = oldNode
		nodeEnabled.Store(oldEnabled)
		dialNode = oldDial
		nodeState.resetStatus()
	})

	nodeState.resetStatus()
	current = nil
	dialNode = func(*runningNode, context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("should not dial")
	}
	nodeEnabled.Store(false)
	if _, err := DialContext(context.Background(), "tcp", "100.94.63.128:9000"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("error = %v, want %v", err, ErrDisabled)
	}
	nodeEnabled.Store(true)
	if _, err := DialContext(context.Background(), "tcp", "100.94.63.128:9000"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("error = %v, want %v", err, ErrNotReady)
	}
}

func TestDialContextRetriesNoState(t *testing.T) {
	oldNode, oldEnabled, oldDial, oldDelay := current, nodeEnabled.Load(), dialNode, noStateRetryDelay
	t.Cleanup(func() {
		current = oldNode
		nodeEnabled.Store(oldEnabled)
		dialNode = oldDial
		noStateRetryDelay = oldDelay
		nodeState.resetStatus()
	})

	nodeState.resetStatus()
	nodeState.update(NodeStatus{State: "running"})
	nodeEnabled.Store(true)
	current = &runningNode{}
	noStateRetryDelay = time.Millisecond
	attempts := 0
	dialNode = func(*runningNode, context.Context, string, string) (net.Conn, error) {
		attempts++
		if attempts < 2 {
			return nil, errors.New("tsnet: backend in state NoState")
		}
		server, client := net.Pipe()
		go client.Close()
		return server, nil
	}
	conn, err := DialContext(context.Background(), "tcp", "100.94.63.128:9000")
	if nil != err || nil == conn {
		t.Fatalf("dial after NoState retry: %v", err)
	}
	conn.Close()
	if 2 != attempts {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDialContextWaitsForStartingThenFails(t *testing.T) {
	oldNode, oldEnabled, oldDial, oldWait, oldPoll := current, nodeEnabled.Load(), dialNode, readyWaitTimeout, readyPollInterval
	t.Cleanup(func() {
		current = oldNode
		nodeEnabled.Store(oldEnabled)
		dialNode = oldDial
		readyWaitTimeout = oldWait
		readyPollInterval = oldPoll
		nodeState.resetStatus()
	})

	nodeState.resetStatus()
	nodeState.update(NodeStatus{State: "starting"})
	nodeEnabled.Store(true)
	current = &runningNode{}
	readyWaitTimeout = 10 * time.Millisecond
	readyPollInterval = time.Millisecond
	called := false
	dialNode = func(*runningNode, context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not dial before running")
	}
	if _, err := DialContext(context.Background(), "tcp", "100.94.63.128:9000"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("error = %v, want %v", err, ErrNotReady)
	}
	if called {
		t.Fatal("starting state must not dial before the node is running")
	}
	if !nodeState.consumePendingRetry() {
		t.Fatal("starting timeout should request a retry")
	}
}

func TestDialContextMarksRetryOnCallerTimeout(t *testing.T) {
	oldNode, oldEnabled, oldDial := current, nodeEnabled.Load(), dialNode
	t.Cleanup(func() {
		current = oldNode
		nodeEnabled.Store(oldEnabled)
		dialNode = oldDial
		nodeState.resetStatus()
	})

	nodeState.resetStatus()
	nodeState.update(NodeStatus{State: "running"})
	nodeEnabled.Store(true)
	current = &runningNode{}
	dialNode = func(_ *runningNode, ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := DialContext(ctx, "tcp", "100.94.63.128:9000"); nil == err {
		t.Fatal("caller timeout should fail the dial")
	}
	if !nodeState.consumePendingRetry() {
		t.Fatal("caller timeout should request a retry")
	}
}

func TestNewSyncTransportModes(t *testing.T) {
	oldHome := util.HomeDir
	util.HomeDir = t.TempDir()
	t.Cleanup(func() { util.HomeDir = oldHome })

	for _, test := range []struct {
		mode      string
		want      string
		wantProxy bool
	}{{SyncNetworkDirect, "direct", true}, {SyncNetworkProxy, "proxy", false}} {
		config := &Config{Enabled: boolPtr(true), Hostname: "node", Listen: "127.0.0.1:2055", SyncNetwork: test.mode}
		config.normalize(ConfigDir())
		if err := writeConfigFile(ConfigDir(), config); nil != err {
			t.Fatalf("write config: %s", err)
		}
		transport := NewSyncTransport(false)
		if transport.Mode() != test.want {
			t.Fatalf("Mode() = %q, want %q", transport.Mode(), test.want)
		}
		if test.wantProxy != (nil != transport.Proxy) {
			t.Fatalf("mode %s proxy present = %v, want %v", test.mode, nil != transport.Proxy, test.wantProxy)
		}
	}
}
