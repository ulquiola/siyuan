// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"sync"

	"github.com/siyuan-note/siyuan/kernel/tailscale"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var (
	tailscaleNoticeMu     sync.Mutex
	tailscaleNoticeID     string
	tailscaleNoticeWanted bool

	syncBlocked           = tailscale.IsBlocked
	consumeTailscaleRetry = tailscale.ConsumePendingRetry

	// scheduleTailscaleNotice 等界面加载后再推送，避免启动同步的提示在界面就绪前被丢弃。
	scheduleTailscaleNotice = func(callback func()) {
		go func() {
			util.WaitForUILoaded()
			callback()
		}()
	}
	pushTailscaleNotice = func(message string) string { return util.PushErrMsg(message, 0) }

	// defaultTailscaleSync 在节点恢复后补一次完整同步；同步锁会合并并发请求，不会叠加执行。
	defaultTailscaleSync = func() { go func() { _ = SyncData(false) }() }
	triggerTailscaleSync = defaultTailscaleSync
)

// showTailscaleSyncNotice 只保留一条常驻提示，避免节点未登录时每次自动同步都弹一条失败。
func showTailscaleSyncNotice() {
	tailscaleNoticeMu.Lock()
	tailscaleNoticeWanted = true
	if "" != tailscaleNoticeID {
		tailscaleNoticeMu.Unlock()
		return
	}
	tailscaleNoticeMu.Unlock()

	scheduleTailscaleNotice(func() {
		tailscaleNoticeMu.Lock()
		defer tailscaleNoticeMu.Unlock()
		if !tailscaleNoticeWanted || "" != tailscaleNoticeID {
			return
		}
		tailscaleNoticeID = pushTailscaleNotice(tailscale.SyncBlockedMessage())
	})
}

func hideTailscaleSyncNotice() {
	tailscaleNoticeMu.Lock()
	defer tailscaleNoticeMu.Unlock()
	tailscaleNoticeWanted = false
	if "" == tailscaleNoticeID {
		return
	}
	util.PushClearMsg(tailscaleNoticeID)
	tailscaleNoticeID = ""
}

// HideTailscaleSyncNotice 供同步模式切回直连时撤销登录提示。
func HideTailscaleSyncNotice() {
	hideTailscaleSyncNotice()
}

// onTailscaleReady 在节点首次进入 running 时执行：有失败同步就清掉提示、解除暂停并补一次同步。
func onTailscaleReady() {
	if !consumeTailscaleRetry() {
		return
	}
	resetAutoSyncErrors()
	hideTailscaleSyncNotice()
	triggerTailscaleSync()
}
