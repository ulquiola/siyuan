// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package tailscale

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
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

	// SyncNetworkDirect 表示同步请求普通直连，不使用内嵌节点；SyncNetworkProxy 表示经内嵌节点拨号。
	SyncNetworkDirect = "direct"
	SyncNetworkProxy  = "proxy"
)

// NodeStatus 是内嵌节点对外暴露的运行状态，state 取值 disabled、starting、needsLogin、needsMachineAuth、running 或 error。
type NodeStatus struct {
	State     string   `json:"state"`
	AuthURL   string   `json:"authURL"`
	IPs       []string `json:"ips"`
	Error     string   `json:"error"`
	KeyExpiry string   `json:"keyExpiry"` // 节点密钥到期时间，RFC3339；未知为空
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
	SyncNetwork  string `json:"syncNetwork"`
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
	c.SyncNetwork = strings.TrimSpace(c.SyncNetwork)
	if "" == c.Hostname {
		c.Hostname = defaultHostname
	}
	if "" == c.Listen {
		c.Listen = defaultListen
	}
	if "" == c.StateDir {
		c.StateDir = filepath.Join(configDir, "tailscale")
	}
	if "" == c.SyncNetwork {
		// 启用内嵌节点时默认经节点同步，保持从 SOCKS 代理迁移过来的行为；未启用时默认直连。
		if c.IsEnabled() {
			c.SyncNetwork = SyncNetworkProxy
		} else {
			c.SyncNetwork = SyncNetworkDirect
		}
	}
}

func (c *Config) validate() error {
	if SyncNetworkDirect != c.SyncNetwork && SyncNetworkProxy != c.SyncNetwork {
		return fmt.Errorf("invalid tailscale sync network [%s]", c.SyncNetwork)
	}
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

// SaveConfig 校验并写入配置；只有节点相关字段变化时才异步重建内嵌节点。
func SaveConfig(configDir string, config *Config) error {
	old, _ := LoadConfig(configDir)
	prepared, err := prepareConfig(configDir, config)
	if nil != err {
		return err
	}
	if err = writeConfigFile(configDir, prepared); nil != err {
		return err
	}
	if nodeConfigChanged(old, prepared) {
		go applyConfig(configDir)
	}
	return nil
}

// applyConfig 是 Apply 的测试替身入口，保存配置时按节点字段是否变化决定是否调用。
var applyConfig = Apply

// nodeConfigChanged 判断保存配置是否需要重建节点；同步网络只影响 S3/WebDAV 传输，不重启节点。
func nodeConfigChanged(old, next *Config) bool {
	if nil == old || nil == next {
		return true
	}
	if old.IsEnabled() != next.IsEnabled() {
		return true
	}
	return old.Hostname != next.Hostname || old.AuthKey != next.AuthKey || old.Listen != next.Listen || old.StateDir != next.StateDir
}

// SyncNetworkMode 返回当前同步网络模式，配置缺失时按直连处理。
func SyncNetworkMode() string {
	return syncNetworkMode(ConfigDir())
}

func syncNetworkMode(configDir string) string {
	config, err := LoadConfig(configDir)
	if nil != err || nil == config {
		return SyncNetworkDirect
	}
	return config.SyncNetwork
}

// IsEmbeddedProxy 判断代理地址是否指向内嵌 Tailscale 的 SOCKS 监听；该地址只服务外部工具，不再作为全局代理。
func IsEmbeddedProxy(proxyURL string) bool {
	return isEmbeddedProxy(ConfigDir(), proxyURL)
}

func isEmbeddedProxy(configDir, proxyURL string) bool {
	proxyURL = strings.TrimSpace(proxyURL)
	if "" == proxyURL {
		return false
	}
	config, err := LoadConfig(configDir)
	if nil != err || nil == config {
		return false
	}
	parsed, err := url.Parse(proxyURL)
	if nil != err {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if "socks5" != scheme && "socks5h" != scheme {
		return false
	}
	listenHost, listenPort, err := net.SplitHostPort(config.Listen)
	if nil != err || parsed.Port() != listenPort {
		return false
	}
	return sameLoopbackHost(parsed.Hostname(), listenHost)
}

// sameLoopbackHost 把 localhost、127.0.0.1 与 ::1 视为同一回环主机。
func sameLoopbackHost(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	isLoopback := func(host string) bool {
		if "localhost" == strings.ToLower(host) {
			return true
		}
		ip := net.ParseIP(host)
		return nil != ip && ip.IsLoopback()
	}
	return isLoopback(a) && isLoopback(b)
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
