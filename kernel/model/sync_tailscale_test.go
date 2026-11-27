// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"testing"
)

func TestOnTailscaleReadyRetriesOnce(t *testing.T) {
	oldConsume, oldTrigger := consumeTailscaleRetry, triggerTailscaleSync
	t.Cleanup(func() {
		consumeTailscaleRetry = oldConsume
		triggerTailscaleSync = oldTrigger
		hideTailscaleSyncNotice()
	})

	consumed := 0
	triggered := 0
	consumeTailscaleRetry = func() bool {
		consumed++
		return 1 == consumed
	}
	triggerTailscaleSync = func() { triggered++ }
	autoSyncErrCount.Store(9)
	onTailscaleReady()
	onTailscaleReady()
	if 1 != triggered {
		t.Fatalf("sync triggers = %d, want 1", triggered)
	}
	if got := autoSyncErrCount.Load(); 0 != got {
		t.Fatalf("auto sync error count = %d, want 0 after recovery", got)
	}
}

func TestPublishSyncResultShowsOneBlockedNotice(t *testing.T) {
	prepareSyncDirectoryTest(t)
	oldBlocked, oldID := syncBlocked, tailscaleNoticeID
	oldSchedule, oldPush := scheduleTailscaleNotice, pushTailscaleNotice
	t.Cleanup(func() {
		syncBlocked = oldBlocked
		scheduleTailscaleNotice = oldSchedule
		pushTailscaleNotice = oldPush
		hideTailscaleSyncNotice()
		tailscaleNoticeID = oldID
	})

	// 测试环境下界面不会加载，改为同步回调便于断言
	scheduleTailscaleNotice = func(callback func()) { callback() }
	pushTailscaleNotice = func(string) string { return "test-notice-id" }
	tailscaleNoticeID = ""
	syncBlocked = func() bool { return true }
	publishSyncResult(2, errors.New("socks dial failed"))
	first := tailscaleNoticeID
	publishSyncResult(2, errors.New("socks dial failed"))
	if "" == first || first != tailscaleNoticeID {
		t.Fatalf("notice ID should stay stable, first=%q now=%q", first, tailscaleNoticeID)
	}
	publishSyncResult(1, nil)
	if "" != tailscaleNoticeID {
		t.Fatal("success should clear the notice")
	}
}

func TestRecordAutoSyncFailureSkipsBlocked(t *testing.T) {
	oldBlocked := syncBlocked
	t.Cleanup(func() { syncBlocked = oldBlocked })

	syncBlocked = func() bool { return true }
	autoSyncErrCount.Store(5)
	recordAutoSyncFailure(errors.New("socks dial failed"))
	if got := autoSyncErrCount.Load(); 5 != got {
		t.Fatalf("auto sync error count = %d, want 5 while blocked", got)
	}
	syncBlocked = func() bool { return false }
	recordAutoSyncFailure(errors.New("network error"))
	if got := autoSyncErrCount.Load(); 6 != got {
		t.Fatalf("auto sync error count = %d, want 6 after one unblocked failure", got)
	}
}

func TestPushSyncErrorSuppressedWhenBlocked(t *testing.T) {
	oldBlocked := syncBlocked
	t.Cleanup(func() { syncBlocked = oldBlocked })

	err := errors.New("socks dial failed")
	syncBlocked = func() bool { return true }
	got := pushSyncError("sync failed", err)
	if got != err {
		t.Fatalf("blocked sync should return the raw error, got %v", got)
	}
	if _, pushed := SyncErrorNoticeFor(got); pushed {
		t.Fatal("blocked sync should not push a notice")
	}
	syncBlocked = func() bool { return false }
	got = pushSyncError("sync failed", err)
	if got == err {
		t.Fatal("unblocked sync should wrap the error with a notice")
	}
	if _, pushed := SyncErrorNoticeFor(got); !pushed {
		t.Fatal("unblocked sync should carry a notice")
	}
}
