// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build android || ios

package tailscale

// Apply 在移动端是空实现：移动端由系统级 Tailscale 客户端处理网络，避免链接完整的用户态网络栈。
// 鸿蒙内核以 GOOS=android 构建，同样使用这里的空实现。
func Apply(configDir string) {
	_ = configDir
}

// Status 在移动端始终返回未启用。
func Status() NodeStatus {
	return NodeStatus{State: "disabled"}
}

// StatusFor 在移动端直接返回 Status：移动端没有内嵌节点，不存在尚未建好的等待过程。
func StatusFor(config *Config) NodeStatus {
	_ = config
	return Status()
}
