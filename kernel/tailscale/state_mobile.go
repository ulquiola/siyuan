// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build android || ios

package tailscale

// 移动端没有内嵌节点，状态回调、阻断判断和待补同步都是空实现，保证共享的 model 代码可以编译。

func SetOnRunning(func()) {}

func IsBlocked() bool { return false }

func SetPendingRetry() {}

func ConsumePendingRetry() bool { return false }

func SyncBlockedMessage() string { return "" }
