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
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/siyuan-note/siyuan/kernel/util"
)

const (
	configFileName  = "tailscale.json"
	defaultHostname = "siyuan-desktop"
	defaultListen   = "127.0.0.1:1055"
)

// NodeStatus 是内嵌节点对外暴露的运行状态，state 取值 disabled、starting、needsLogin、running 或 error。
type NodeStatus struct {
	State   string   `json:"state"`
	AuthURL string   `json:"authURL"`
	IPs     []string `json:"ips"`
	Error   string   `json:"error"`
}

// Config 是配置目录下 tailscale.json 的结构；文件不存在时内嵌 Tailscale 整体关闭。
//
// 配置单独存放而不是写入 conf.json：认证密钥和节点状态不适合进入可导出的设置，
// 也避免与设置合并语义耦合；界面只通过 getTailscale/setTailscale 读写。
type Config struct {
	Enabled      *bool  `json:"enabled"`  // 省略时视为启用，便于只写必要字段
	Hostname     string `json:"hostname"` // 加入 Tailnet 时的节点名
	AuthKey      string `json:"authKey"`  // 首次加入使用的认证密钥，已加入后可留空
	ClearAuthKey bool   `json:"-"`        // 仅用于请求，为 true 时删除已保存的密钥
	Listen       string `json:"listen"`   // SOCKS5 监听地址，仅允许回环地址
	StateDir     string `json:"stateDir"` // 节点状态目录，留空使用配置目录下的 tailscale
}

// LoadConfig 读取配置目录下的 tailscale.json。
//
// 配置文件不存在时返回 nil, nil，表示该功能未启用，不影响默认启动行为。
func LoadConfig(configDir string) (*Config, error) {
	configPath := filepath.Join(configDir, configFileName)
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if nil != err {
		return nil, fmt.Errorf("read tailscale config [%s] failed: %w", configPath, err)
	}

	config := &Config{}
	if err = json.Unmarshal(data, config); nil != err {
		return nil, fmt.Errorf("parse tailscale config [%s] failed: %w", configPath, err)
	}
	config.normalize(configDir)
	if err = config.validate(); nil != err {
		return nil, err
	}
	return config, nil
}

func (c *Config) normalize(configDir string) {
	c.Hostname = strings.TrimSpace(c.Hostname)
	c.AuthKey = strings.TrimSpace(c.AuthKey)
	c.Listen = strings.TrimSpace(c.Listen)
	c.StateDir = strings.TrimSpace(c.StateDir)
	if "" == c.Hostname {
		c.Hostname = defaultHostname
	}
	if "" == c.Listen {
		c.Listen = defaultListen
	}
	if "" == c.StateDir {
		c.StateDir = filepath.Join(configDir, "tailscale")
	}
}

func (c *Config) validate() error {
	host, port, err := net.SplitHostPort(c.Listen)
	if nil != err {
		return fmt.Errorf("invalid tailscale listen address [%s]: %w", c.Listen, err)
	}
	portNumber, err := strconv.Atoi(port)
	if nil != err || 1 > portNumber || 65535 < portNumber {
		return fmt.Errorf("tailscale listen port [%s] must be between 1 and 65535", port)
	}
	if "localhost" == host {
		return nil
	}
	if ip := net.ParseIP(host); nil == ip || !ip.IsLoopback() {
		// SOCKS5 可以访问整个 Tailnet，监听非回环地址会把节点网络暴露给局域网
		return fmt.Errorf("tailscale listen address [%s] must be a loopback address", c.Listen)
	}
	return nil
}

// IsEnabled 返回配置是否启用，省略 enabled 字段时视为启用。
func (c *Config) IsEnabled() bool {
	return nil == c.Enabled || *c.Enabled
}

// ConfigDir 返回用户级配置目录，与内核日志使用同一位置。
func ConfigDir() string {
	return filepath.Join(util.HomeDir, ".config", "siyuan")
}

// NewConfig 返回带默认值的配置，用于尚未保存过配置时展示；配置缺失时界面默认显示为未启用。
func NewConfig(configDir string) *Config {
	enabled := false
	config := &Config{Enabled: &enabled}
	config.normalize(configDir)
	return config
}

// SaveConfig 校验并写入配置，随后异步重建内嵌节点。
func SaveConfig(configDir string, config *Config) error {
	prepared, err := prepareConfig(configDir, config)
	if nil != err {
		return err
	}
	if err = writeConfigFile(configDir, prepared); nil != err {
		return err
	}
	go Apply(configDir)
	return nil
}

// prepareConfig 归一化并校验配置，界面不编辑状态目录，请求中的空值表示沿用已有配置或默认值。
func prepareConfig(configDir string, config *Config) (*Config, error) {
	if config.ClearAuthKey {
		config.AuthKey = ""
	}
	if existing, err := LoadConfig(configDir); nil == err && nil != existing {
		if "" == strings.TrimSpace(config.StateDir) {
			config.StateDir = existing.StateDir
		}
		// 界面不再回填已保存的密钥，空值表示沿用现有密钥；显式清除时保持为空
		if "" == strings.TrimSpace(config.AuthKey) && !config.ClearAuthKey {
			config.AuthKey = existing.AuthKey
		}
	}
	config.normalize(configDir)
	if err := config.validate(); nil != err {
		return nil, err
	}
	return config, nil
}

func writeConfigFile(configDir string, config *Config) error {
	if err := os.MkdirAll(configDir, 0755); nil != err {
		return fmt.Errorf("create config dir [%s] failed: %w", configDir, err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if nil != err {
		return fmt.Errorf("marshal tailscale config failed: %w", err)
	}
	data = append(data, '\n')

	// 临时文件名唯一，避免并发保存时互相覆盖；CreateTemp 默认权限为 0600
	tmp, err := os.CreateTemp(configDir, configFileName+".tmp-*")
	if nil != err {
		return fmt.Errorf("create temporary tailscale config failed: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.Write(data); nil != err {
		tmp.Close()
		return fmt.Errorf("write tailscale config [%s] failed: %w", tmpPath, err)
	}
	if err = tmp.Sync(); nil != err {
		tmp.Close()
		return fmt.Errorf("sync tailscale config [%s] failed: %w", tmpPath, err)
	}
	if err = tmp.Close(); nil != err {
		return fmt.Errorf("close tailscale config [%s] failed: %w", tmpPath, err)
	}

	configPath := filepath.Join(configDir, configFileName)
	if err = os.Rename(tmpPath, configPath); nil != err {
		return fmt.Errorf("replace tailscale config [%s] failed: %w", configPath, err)
	}
	return nil
}
