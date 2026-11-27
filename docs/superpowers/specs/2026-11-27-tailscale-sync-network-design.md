# 内嵌 Tailscale：同步网络直连/代理开关与登录闭环

## 意图

用户在 3.8.12 上看到 S3 同步失败，但服务器/MinIO 本身是好的：SiYuan 的 S3/WebDAV 请求被全局网络代理 `socks5://127.0.0.1:1055`（内嵌 Tailscale 的 SOCKS 监听）接管，而该监听背后的 `tsnet.Dial` 在网络切换、唤醒后的冷路径上超过 5 秒就会被判失败；同一个全局代理还会把版本检查、集市等无关流量拖进内嵌节点，节点没起来时这些请求也跟着报错。

用户要的是：

- S3/WebDAV 同步可以显式选择「直连」或「代理」（代理 = 内嵌 Tailscale 节点本身），其他流量一律直连；
- 代理模式不再受 SOCKS 的 5 秒限制；
- 未登录、待管理员批准、登录过期时快速给出明确状态；登录完成后同步自动恢复。

## 现状（已核实）

- `kernel/tailscale/embed.go` 用 `serveSOCKS(listener, server.Dial)` 把 1055 的每个连接交给 `tsnet.Server.Dial`；`tailscale.com/net/socks5` 固定 5 秒拨号超时（`net/socks5/socks5.go:209`）。
- `D:\WorkSpace\SiYuan\conf\conf.json` 的 `system.networkProxy` 为 `socks5://127.0.0.1:1055`；`kernel/model/conf.go:579` 在启动时调用 `util.SetNetworkProxy` 写入 `HTTP_PROXY/HTTPS_PROXY`。
- dejavu 的 S3 客户端在 `kernel/model/repository.go:3276` 用 `httpclient.NewTransport`（`Proxy: ProxyFromEnvironment`）创建；WebDAV 在 `:3286`；测试连接在 `kernel/model/sync_probe.go:91`。
- `D:\WorkSpace\SiYuan\temp\siyuan.log` 显示升级前的 3.8.9 构建已有同样的 `socks connect tcp 127.0.0.1:1055->100.94.63.128:9000 ... general SOCKS server failure`；3.8.12 的 #21644 只是把失败通知出来。
- `tsnet.Server.Dial` 自带 `awaitRunning`，但 `NoState` 会被当成终止状态立即报 `backend in state NoState`；`tsnet` 在 `NeedsLogin` 时会自动发起交互式登录并打印登录链接。
- `statusFromState` 把 `NeedsMachineAuth` 映射成 `needsLogin`；没有密钥过期信息；设置页固定 3 秒轮询。
- 1055 监听可以保留给外部工具；SiYuan 自身不再需要它。

## 目标

1. `tailscale.json` 新增 `syncNetwork`：`direct`（普通直连，不用代理、不用内嵌节点）或 `proxy`（经内嵌 Tailscale 节点，`tsnet.Dial`）；只影响 S3 + WebDAV。
2. 其他内核 HTTP 不再经内嵌节点；如果全局网络代理仍指向内嵌监听地址，内核忽略它并按直连处理。
3. 代理模式使用同步客户端自己的超时（S3 的 `timeout`），不再受 SOCKS 5 秒限制；`NoState` 短暂重试。
4. 登录闭环：状态区分 `needsLogin` / `needsMachineAuth` / 密钥过期；未登录时快速失败；节点变为 `running` 时自动补一次同步；同一次登录会话只推一条常驻提示。
5. 只改 `syncNetwork` 不重启节点。

## 非目标

- 不改 dejavu、不删除 1055 SOCKS 监听（外部工具仍可用）。
- 不做退出登录/切换账号；不改官方 SiYuan 云同步 provider；不新增 Headscale `ControlURL`。
- 不把登录链接放进同步通知或错误文案。
- 不做 WebSocket 推送或系统通知。

## 反目标（功能跑通也算失败）

- 其他流量（bazaar、AI、版本检查、公告）仍被塞进内嵌节点。
- 登录成功后仍显示旧链接，或只改开关就把节点重启/踢下线。
- 未登录时先干等 60 秒；每次自动同步都弹一条失败。
- 为了直连把系统/真实代理也一起关掉：真实代理设置必须继续生效。

## 约束

- Go 改动后 `gofmt -w`；`go test` 允许，不编译内核、不重启内核。
- 前端只跑 `cd app && pnpm run lint`（含 typecheck）；不跑 `pnpm build` / `pnpm dev`。
- i18n 新键加在每个 `app/appearance/langs/*.json` 顶部（23 个文件，各自翻译），改完跑 `python scripts/check-lang-keys.py`。
- API 契约先改 `kernel/apicontract`，再 `cd app && pnpm run api:generate` / `pnpm run api:check`；不手改生成文件。
- 注释中文、120 字符换行；不新增第三方依赖。
- `git commit` 必须取得用户明确同意后执行。
- 这个 fork 要持续合并上游：配置放 `tailscale.json`、UI 放 fork 自己的 Tailscale 设置页，尽量不动上游同步设置。

## 方案

- **A 自动识别 tailnet 地址**：在现有 transport 里判断 `100.64.0.0/10`、`fd7a:115c:a1e0::/48`、`*.ts.net`。改动小，但用户要的是显式开关，且「直连」与「代理」的语义无法由地址推断。
- **B 显式 `syncNetwork` 开关（选定）**：S3/WebDAV 二选一：直连（`Proxy: nil` + 普通拨号）或代理（`Proxy: nil` + `tsnet.Dial`）。其他流量不受影响。
- **C 保留全局代理 + NO_PROXY**：5 秒 SOCKS 限制仍在，且全局代理的副作用已经证明会拖死版本检查，不能接受。

登录方案不做「等超时后报错」，而是「状态缓存 → 快速失败 → 登录后自动补同步 → 单条常驻提示」。

## 落实细节（已定）

### 配置

`kernel/tailscale/config.go`：

- `Config` 新增 `SyncNetwork string`（JSON `syncNetwork`），取值 `direct` / `proxy`。
- `normalize`：空值时，`IsEnabled()` 为 true → `proxy`，否则 `direct`；`validate` 拒绝非法值。
- `SaveConfig`：比较旧配置的节点字段（Enabled/Hostname/AuthKey/Listen/StateDir），只有这些变化才 `go Apply`；`syncNetwork` 只更新文件。
- 新增 `SyncNetworkMode() string`（每次读取配置文件，缺省 `direct`）与 `IsEmbeddedProxy(proxyURL string) bool`（scheme 为 socks5/socks5h 且 host:port 等于 Listen；`localhost` 与 `127.0.0.1` 视为等价）。

### 拨号与传输

`kernel/tailscale/transport.go`（新）：

- `DialContext(ctx, network, address) (net.Conn, error)`：
  - 无当前节点：配置未启用 → `ErrDisabled`；已启用但未建好 → `ErrNotReady`。
  - 缓存状态 `needsLogin` → `ErrNeedsLogin`；`needsMachineAuth` → `ErrNeedsMachineAuth`（均带本地化文案）。
  - 否则 `current.server.Dial`；`NoState` 时按未就绪重试（最多 2 次、每次 200ms），仍失败返回 `ErrNotReady`。
  - 返回 blocked 类错误时置位 `pendingRetry`。
- `NewSyncTransport(skipTLSVerify bool) *SyncTransport`：`SyncTransport` 内嵌 `*http.Transport`，带 `Mode()`；两种模式都 `Proxy = nil`（忽略全局 env 代理）；`proxy` 模式 `DialContext = DialContext`，`direct` 模式用普通 `net.Dialer`。
- 接入 `kernel/model/repository.go` 的 S3/WebDAV 与 `kernel/model/sync_probe.go`。

### 状态缓存与登录

`kernel/tailscale`：

- `NodeStatus` 增加 `KeyExpiry string`（RFC3339）；`statusFromState` 增加 `needsMachineAuth`，并从 `Self.KeyExpiry` 取到期时间。
- 新增 `stateCache`：`update` 返回是否首次进入 running；`snapshot`、`IsBlocked`、`SetPendingRetry`、`ConsumePendingRetry`、`SetOnRunning`。
- `Apply` 启动节点后启动 `watchNode`：2 秒轮询（running 时 10 秒），更新缓存；首次进入 running 时回调。
- `Status()` 保持实时查询；查询成功后顺手更新缓存。

### 自动补同步与提示

`kernel/model`：

- `publishSyncResult`：失败且 `tailscale.IsBlocked()` 时，`Stat` 使用 `tailscale.SyncBlockedMessage()`，并只推一条 `PushErrMsg(msg, 0)` 常驻提示；成功时清掉。
- 注册 `tailscale.SetOnRunning`：`ConsumePendingRetry()` 为 true 时清提示并执行一次 `SyncData(false)`。
- `model.applyNetworkProxy()`：全局代理指向内嵌监听地址时按直连应用（`""`），真实代理/系统代理不变；`InitConf`、`conf_specs`、`setTailscale` 调用它。

### API 与界面

- `apicontract.TailscaleConfig` / `TailscaleConfigRequest` 增加 `syncNetwork`；`TailscaleStatus` 增加 `keyExpiry`；`kernel/api/system.go` 映射；重新生成前端类型。
- `app/src/config/tabs/appTab.ts`：Tailscale 页新增「同步网络」下拉（直连/代理）；`save` 带上 `syncNetwork`；`needsLogin` / `starting` / `needsMachineAuth` 时 2 秒轮询，`running` / `disabled` 时 10 秒。
- `app/src/config/tabs/tailscaleStatus.ts`：`needsMachineAuth`、密钥过期/7 天内过期文案；测试补矩阵。
- i18n 新键：`tailscaleNeedsMachineAuth`、`tailscaleKeyExpired`、`tailscaleKeyExpiring`、`tailscaleSyncNetwork`、`tailscaleSyncNetworkDirect`、`tailscaleSyncNetworkProxy`、`tailscaleSyncNetworkTip`、`tailscaleSyncBlocked`。

## 留给实现者的空间

- 状态错误的具体措辞、轮询间隔、`KeyExpiry` 的展示格式。
- 是否在设置页提供管理后台链接（`https://login.tailscale.com/admin/machines`）。
- `SyncTransport` 的文件内布局与测试注入方式。