// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/tailscale"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestEffectiveNetworkProxyIgnoresEmbeddedListener(t *testing.T) {
	oldHome := util.HomeDir
	util.HomeDir = t.TempDir()
	t.Cleanup(func() { util.HomeDir = oldHome })
	configDir := tailscale.ConfigDir()
	if err := os.MkdirAll(configDir, 0755); nil != err {
		t.Fatalf("create config dir: %s", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tailscale.json"), []byte(`{"enabled":true,"listen":"127.0.0.1:1055"}`), 0600); nil != err {
		t.Fatalf("write config: %s", err)
	}

	value, useSystem := effectiveNetworkProxy(&conf.NetworkProxy{Scheme: "socks5", Host: "127.0.0.1", Port: "1055"})
	if "" != value || useSystem {
		t.Fatalf("embedded listener should be ignored, got %q system=%v", value, useSystem)
	}
	value, _ = effectiveNetworkProxy(&conf.NetworkProxy{Scheme: "socks5", Host: "127.0.0.1", Port: "1080"})
	if "socks5://127.0.0.1:1080" != value {
		t.Fatalf("real proxy should be preserved, got %q", value)
	}
	_, useSystem = effectiveNetworkProxy(&conf.NetworkProxy{Scheme: "system"})
	if !useSystem {
		t.Fatal("system proxy should stay system")
	}
}
