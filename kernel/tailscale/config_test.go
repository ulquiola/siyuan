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

package tailscale

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, configFileName), []byte(content), 0600); nil != err {
		t.Fatalf("write config: %s", err)
	}
	return configDir
}

func TestLoadConfigMissingFile(t *testing.T) {
	config, err := LoadConfig(t.TempDir())
	if nil != err {
		t.Fatalf("unexpected error: %s", err)
	}
	if nil != config {
		t.Fatalf("expected disabled feature, got %#v", config)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	configDir := writeConfig(t, "{}")
	config, err := LoadConfig(configDir)
	if nil != err {
		t.Fatalf("load config: %s", err)
	}
	if nil == config || !config.IsEnabled() {
		t.Fatalf("expected enabled config, got %#v", config)
	}
	if defaultHostname != config.Hostname {
		t.Fatalf("hostname = %q, want %q", config.Hostname, defaultHostname)
	}
	if defaultListen != config.Listen {
		t.Fatalf("listen = %q, want %q", config.Listen, defaultListen)
	}
	if want := filepath.Join(configDir, "tailscale"); want != config.StateDir {
		t.Fatalf("state dir = %q, want %q", config.StateDir, want)
	}
}

func TestLoadConfigExplicitValues(t *testing.T) {
	configDir := writeConfig(t, `{"hostname":" laptop ","authKey":" tskey-auth-1 ","listen":"127.0.0.1:2055","stateDir":" /tmp/ts "}`)
	config, err := LoadConfig(configDir)
	if nil != err {
		t.Fatalf("load config: %s", err)
	}
	if "laptop" != config.Hostname {
		t.Fatalf("hostname = %q", config.Hostname)
	}
	if "tskey-auth-1" != config.AuthKey {
		t.Fatalf("auth key = %q", config.AuthKey)
	}
	if "127.0.0.1:2055" != config.Listen {
		t.Fatalf("listen = %q", config.Listen)
	}
	if "/tmp/ts" != config.StateDir {
		t.Fatalf("state dir = %q", config.StateDir)
	}
}

func TestLoadConfigDisabled(t *testing.T) {
	configDir := writeConfig(t, `{"enabled":false}`)
	config, err := LoadConfig(configDir)
	if nil != err {
		t.Fatalf("load config: %s", err)
	}
	if nil == config || config.IsEnabled() {
		t.Fatalf("expected disabled config, got %#v", config)
	}
}

func TestLoadConfigLocalhostListen(t *testing.T) {
	configDir := writeConfig(t, `{"listen":"localhost:1055"}`)
	if _, err := LoadConfig(configDir); nil != err {
		t.Fatalf("localhost should be accepted: %s", err)
	}
}

func TestLoadConfigRejectsNonLoopbackListen(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:1055", "192.168.1.2:1055", ":1055"} {
		configDir := writeConfig(t, `{"listen":"`+listen+`"}`)
		if _, err := LoadConfig(configDir); nil == err {
			t.Fatalf("listen %q should be rejected", listen)
		}
	}
}

func TestLoadConfigRejectsInvalidPort(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:", "127.0.0.1:0", "127.0.0.1:70000", "127.0.0.1:port"} {
		configDir := writeConfig(t, `{"listen":"`+listen+`"}`)
		if _, err := LoadConfig(configDir); nil == err {
			t.Fatalf("listen %q should be rejected", listen)
		}
	}
}

func TestLoadConfigInvalidContent(t *testing.T) {
	if _, err := LoadConfig(writeConfig(t, "{")); nil == err {
		t.Fatal("invalid JSON should fail")
	}
	configDir := writeConfig(t, `{"listen":"not-an-address"}`)
	if _, err := LoadConfig(configDir); nil == err {
		t.Fatal("invalid listen address should fail")
	}
}

func TestNewConfigDisabledByDefault(t *testing.T) {
	config := NewConfig(t.TempDir())
	if nil == config || config.IsEnabled() {
		t.Fatalf("expected disabled default config, got %#v", config)
	}
	if defaultHostname != config.Hostname || defaultListen != config.Listen {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestPrepareConfigInheritsStateDir(t *testing.T) {
	configDir := writeConfig(t, `{"stateDir":"/custom/state"}`)
	enabled := true
	config, err := prepareConfig(configDir, &Config{Enabled: &enabled, Listen: "127.0.0.1:2055"})
	if nil != err {
		t.Fatalf("prepare config: %s", err)
	}
	if "/custom/state" != config.StateDir {
		t.Fatalf("state dir = %q, want inherited value", config.StateDir)
	}
	if defaultHostname != config.Hostname {
		t.Fatalf("hostname = %q, want %q", config.Hostname, defaultHostname)
	}
}

func TestPrepareConfigKeepsSavedAuthKey(t *testing.T) {
	configDir := writeConfig(t, `{"authKey":"tskey-saved"}`)
	config, err := prepareConfig(configDir, &Config{Listen: "127.0.0.1:2055"})
	if nil != err {
		t.Fatalf("prepare config: %s", err)
	}
	if "tskey-saved" != config.AuthKey {
		t.Fatalf("auth key = %q, want inherited value", config.AuthKey)
	}

	config, err = prepareConfig(configDir, &Config{AuthKey: "tskey-new", Listen: "127.0.0.1:2055"})
	if nil != err {
		t.Fatalf("prepare config: %s", err)
	}
	if "tskey-new" != config.AuthKey {
		t.Fatalf("auth key = %q, want new value", config.AuthKey)
	}
}

func TestPrepareConfigClearsSavedAuthKey(t *testing.T) {
	configDir := writeConfig(t, `{"authKey":"tskey-saved"}`)
	config, err := prepareConfig(configDir, &Config{ClearAuthKey: true, AuthKey: "tskey-typed", Listen: "127.0.0.1:2055"})
	if nil != err {
		t.Fatalf("prepare config: %s", err)
	}
	if "" != config.AuthKey {
		t.Fatalf("auth key = %q, want cleared value", config.AuthKey)
	}
}

func TestPrepareConfigUsesDefaultStateDir(t *testing.T) {
	configDir := t.TempDir()
	config, err := prepareConfig(configDir, &Config{})
	if nil != err {
		t.Fatalf("prepare config: %s", err)
	}
	if want := filepath.Join(configDir, "tailscale"); want != config.StateDir {
		t.Fatalf("state dir = %q, want %q", config.StateDir, want)
	}
}

func TestPrepareConfigRejectsNonLoopbackListen(t *testing.T) {
	if _, err := prepareConfig(t.TempDir(), &Config{Listen: "0.0.0.0:1055"}); nil == err {
		t.Fatal("non-loopback listen address should be rejected")
	}
}

func TestWriteConfigRoundTrip(t *testing.T) {
	configDir := t.TempDir()
	enabled := false
	config := &Config{Enabled: &enabled, Hostname: "node", AuthKey: "key", Listen: "127.0.0.1:2055"}
	config.normalize(configDir)
	if err := writeConfigFile(configDir, config); nil != err {
		t.Fatalf("write config: %s", err)
	}
	loaded, err := LoadConfig(configDir)
	if nil != err {
		t.Fatalf("load config: %s", err)
	}
	if nil == loaded || loaded.IsEnabled() || "node" != loaded.Hostname || "key" != loaded.AuthKey || "127.0.0.1:2055" != loaded.Listen {
		t.Fatalf("unexpected round trip result: %#v", loaded)
	}
	matches, err := filepath.Glob(filepath.Join(configDir, configFileName+".tmp-*"))
	if nil != err || 0 != len(matches) {
		t.Fatalf("temporary config files should not remain: %v, %v", matches, err)
	}
}

func TestWriteConfigConcurrent(t *testing.T) {
	configDir := t.TempDir()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			config := &Config{Hostname: fmt.Sprintf("node-%d", i), Listen: "127.0.0.1:2055"}
			config.normalize(configDir)
			if err := writeConfigFile(configDir, config); nil != err {
				t.Errorf("write config: %s", err)
			}
		}(i)
	}
	wg.Wait()

	if _, err := LoadConfig(configDir); nil != err {
		t.Fatalf("config should stay valid after concurrent writes: %s", err)
	}
	matches, err := filepath.Glob(filepath.Join(configDir, configFileName+".tmp-*"))
	if nil != err || 0 != len(matches) {
		t.Fatalf("temporary config files should not remain: %v, %v", matches, err)
	}
}

func TestStatusWithoutNode(t *testing.T) {
	if status := Status(); "disabled" != status.State {
		t.Fatalf("state = %q, want disabled", status.State)
	}
}
