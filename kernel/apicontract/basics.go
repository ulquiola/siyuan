// SiYuan - https://github.com/siyuan-note/siyuan
// Copyright (c) 2020-present, b3log.org
// SPDX-License-Identifier: AGPL-3.0-or-later

package apicontract

type WorkspaceInfoData struct {
	WorkspaceDir string `json:"workspaceDir"`
	SiyuanVer    string `json:"siyuanVer"`
}

type BootProgressData struct {
	Progress int32  `json:"progress"`
	Details  string `json:"details"`
}

type NetworkProxy struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   string `json:"port"`
}

type NetworkData struct {
	Proxy *NetworkProxy `json:"proxy"`
}

// TailscaleConfig 是内嵌 Tailscale 节点的可编辑配置，状态目录由内核维护，不在界面暴露。
type TailscaleConfig struct {
	Enabled      bool   `json:"enabled"`
	Hostname     string `json:"hostname"`
	AuthKeySaved bool   `json:"authKeySaved"` // 是否已经保存过密钥，密钥本身不会返回
	Listen       string `json:"listen"`
}

// TailscaleConfigRequest 是保存 Tailscale 配置的请求体，不包含只用于响应的 authKeySaved。
type TailscaleConfigRequest struct {
	Enabled      bool   `json:"enabled"`
	Hostname     string `json:"hostname"`
	AuthKey      string `json:"authKey"`      // 为空表示沿用已保存的密钥
	ClearAuthKey bool   `json:"clearAuthKey"` // 为 true 时删除已保存的密钥
	Listen       string `json:"listen"`
}

// TailscaleStatus 是内嵌节点当前的运行状态，state 取值 disabled、starting、needsLogin、running 或 error。
type TailscaleStatus struct {
	State   string   `json:"state"`
	AuthURL string   `json:"authURL"`
	IPs     []string `json:"ips"`
	Error   string   `json:"error"`
}

type TailscaleData struct {
	Config TailscaleConfig `json:"config"`
	Status TailscaleStatus `json:"status"`
}

type LockScreenRequest struct {
	LockScreenMode float64 `json:"lockScreenMode"`
}

type NetworkServeRequest struct {
	NetworkServe bool `json:"networkServe"`
}

type NetworkServeTLSRequest struct {
	NetworkServeTLS bool `json:"networkServeTLS"`
}

type AutoLaunchRequest struct {
	AutoLaunch float64 `json:"autoLaunch"`
}

type DownloadInstallPkgRequest struct {
	DownloadInstallPkg bool `json:"downloadInstallPkg"`
}

// SettingsWindowRequest 更新工作空间的独立设置窗口开关，仅桌面 Electron 客户端使用。
type SettingsWindowRequest struct {
	SettingsWindow bool `json:"settingsWindow"`
}

type UpdateChannelRequest struct {
	UpdateChannel string `json:"updateChannel"`
}

type BlockIDsRequest struct {
	IDs []string `json:"ids"`
}

type DOMTextRequest struct {
	DOM string `json:"dom"`
}

type RemoveBookmarkRequest struct {
	Bookmark string `json:"bookmark"`
}

type RenameBookmarkRequest struct {
	OldBookmark string `json:"oldBookmark"`
	NewBookmark string `json:"newBookmark"`
}

type RemoveTagRequest struct {
	Label string `json:"label"`
}

type RenameTagRequest struct {
	OldLabel string `json:"oldLabel"`
	NewLabel string `json:"newLabel" api:"trim"`
}

type BatchSetBlockAttrsRequest struct {
	BlockAttrs []SetBlockAttrsRequest `json:"blockAttrs"`
}

type EditorReadOnlyRequest struct {
	ReadOnly bool `json:"readonly"`
}

type VirtualBlockRefRequest struct {
	Keywords []string `json:"keywords"`
}

type GetTagRequest struct {
	IgnoreMaxListHint bool    `json:"ignoreMaxListHint" api:"optional,nullable"`
	App               string  `json:"app" api:"optional,nullable"`
	Sort              float64 `json:"sort" api:"optional,nullable"`
}

type TagData struct {
	Name     string     `json:"name"`
	Label    string     `json:"label"`
	Children []*TagData `json:"children"`
	Type     string     `json:"type"`
	Depth    int        `json:"depth"`
	Count    int        `json:"count"`
}
