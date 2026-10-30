// SiYuan - From thought to insight, with agents
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
