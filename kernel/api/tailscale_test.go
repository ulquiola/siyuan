// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/siyuan-note/siyuan/kernel/tailscale"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestTailscaleDataDoesNotExposeSavedAuthKey(t *testing.T) {
	oldHomeDir := util.HomeDir
	util.HomeDir = t.TempDir()
	t.Cleanup(func() { util.HomeDir = oldHomeDir })

	configDir := tailscale.ConfigDir()
	if err := os.MkdirAll(configDir, 0755); nil != err {
		t.Fatalf("create config dir: %s", err)
	}
	configPath := filepath.Join(configDir, "tailscale.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"node","authKey":"tskey-secret","listen":"127.0.0.1:2055"}`), 0600); nil != err {
		t.Fatalf("write config: %s", err)
	}

	data := tailscaleData()
	payload, err := json.Marshal(data)
	if nil != err {
		t.Fatalf("marshal data: %s", err)
	}
	if strings.Contains(string(payload), `"authKey":`) {
		t.Fatalf("auth key should not be returned, got %s", payload)
	}
	if !data.Config.AuthKeySaved {
		t.Fatal("saved auth key should be reported")
	}

	if err := os.Remove(configPath); nil != err {
		t.Fatalf("remove config: %s", err)
	}
	data = tailscaleData()
	if data.Config.AuthKeySaved {
		t.Fatal("missing config should not report a saved auth key")
	}
}
