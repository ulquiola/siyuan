// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/tailscale"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// effectiveNetworkProxy 返回内核实际使用的网络代理。
//
// 指向内嵌 Tailscale 监听地址的代理只服务 S3/WebDAV 的同步传输，不能再作为全局代理，
// 否则版本检查、集市等无关请求会在节点未就绪时一起失败；系统代理和真实代理地址保持原样。
func effectiveNetworkProxy(proxy *conf.NetworkProxy) (value string, useSystem bool) {
	if nil == proxy {
		return "", false
	}
	if proxy.IsSystem() {
		return "", true
	}
	value = proxy.String()
	if tailscale.IsEmbeddedProxy(value) {
		return "", false
	}
	return value, false
}

func applyNetworkProxy(proxy *conf.NetworkProxy) {
	value, useSystem := effectiveNetworkProxy(proxy)
	if nil != proxy && "" != proxy.String() && "" == value && !useSystem {
		logging.LogInfof("ignore embedded tailscale proxy [%s] for kernel requests", proxy.String())
	}
	util.SetNetworkProxy(value, useSystem)
}

// ApplyNetworkProxy 在 Tailscale 配置或网络代理设置变化后重新应用内核代理。
func ApplyNetworkProxy() {
	applyNetworkProxy(Conf.System.Load().NetworkProxy)
}
