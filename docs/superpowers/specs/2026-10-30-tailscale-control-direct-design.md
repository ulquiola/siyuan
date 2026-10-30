# 内嵌 Tailscale：控制面固定直连 + 失败原因可见

## 意图

首次登录实测暴露两个缺陷：其一，运行中修改「设置 → 网络代理」对已启动的内嵌 Tailscale 完全无效，必须重启内核；其二，控制面连接失败时界面只有一句「正在等待登录链接，请检查网络或代理」，真实原因只写在内核日志和 `~/.config/siyuan/tailscale/tailscaled.log*.txt` 里。用户因此误判为「登录链接不显示」的 bug 复发。

## 现状（已核实）

- `kernel/util.SetNetworkProxy` 在进程内改 `HTTP_PROXY`/`HTTPS_PROXY` 环境变量，但 Go 的 `httpproxy.FromEnvironment()` 与 Tailscale 的 `tshttpproxy` 都会把配置缓存到首次使用为止，之后改环境变量无效。
- `tshttpproxy.InvalidateCache()` 只清 `noProxyUntil` 退避计时，不重读环境，无法用来刷新配置。
- `tshttpproxy.SetProxyFunc` 可以覆盖代理函数，但要求包级 `config == nil`；现有 `configureSelfProxy()` 调用的 `SetSelfProxy` 会抢先把它初始化。
- 实测日志（`tailscaled.log2.txt`）：切到 `direct` 两秒后仍是 `proxyconnect tcp: dial tcp 172.25.209.110:1055: ...`。
- 隔离探针验证：同机同网、全新 state 目录时 tsnet 两秒内即取得 `https://login.tailscale.com/a/...`，说明网络与登录链路本身无问题。
- Tailscale 的健康告警里有控制面失联项，但 `TimeToVisible` 为 8 分钟，且 `statusFromState` 目前只在 `error` 分支保留 `Health`。

## 目标

1. 内嵌 Tailscale 的控制面连接始终直连，不受「设置 → 网络代理」影响，也不因该设置变化而失效。
2. 节点未连上时，界面能显示控制面失败的真实原因（超时、TLS、代理等），不必再翻日志。
3. 已连接时不显示任何错误残留。

## 非目标

- 不新增控制面地址（Headscale `ControlURL`）配置项。
- 不改 S3 同步、代理设置本身的行为，SiYuan 自己的请求继续遵循网络代理。
- 不把 tsnet 日志文件路径或日志内容整体搬到界面。

## 反目标（跑通也算失败）

- 控制面改成直连后，用户在必须走代理的企业网络里彻底无法使用该功能，却没有任何提示。
- 界面长期挂着一句早已失效的错误，或把已连接状态显示成错误。
- 依赖 tsnet 日志的具体措辞做字符串解析，换版本即失效且无人察觉。

## 约束

- Go 改动后 `gofmt`；不编译内核二进制、不重启内核；`go test` 允许。
- 前端改完只跑 `cd app && pnpm run lint`，不跑 `pnpm build` / `pnpm dev`。
- 注释中文、120 字符换行；不新增 API 契约字段（避免 `api:generate` 与 TS 类型连锁改动）。

## 方案

- **A 调用 `InvalidateCache`**：已否，它不重读环境变量，改完仍走旧代理。
- **B 控制面固定直连（选定）**：在 `Apply()` 最先执行一次 `tshttpproxy.SetProxyFunc(func(*url.URL) (*url.URL, error) { return nil, nil })`，并移除 `configureSelfProxy()` 与 `SetSelfProxy` 调用。既解决配置冻结，也彻底消除「代理指向自己造成死锁」这一整类问题。
- **C 每次代理变更重启节点**：改动小，但重启节点本来就要重启进程才生效（缓存问题依旧），且用户切代理时会被踢下线，体验更差。

选 B 的理由：Tailscale 是 VPN 客户端，其控制面不应骑在应用层代理上；B 用一次覆盖把「配置冻结」和「自指死锁」两个问题一起消掉，代码比现状更少。

## 落实细节（已定）

后端（`kernel/tailscale/embed.go`）：

- 新增 `ensureControlProxyDirect()`，用 `sync.Once` 保证只装一次；`Apply()` 开头调用它。若 `SetProxyFunc` 返回错误（`config` 已被别的路径初始化），记一条 warn 日志并继续，不阻断节点启动。
- 删除 `configureSelfProxy()` 及其调用与注释；`embed_test.go` 中依赖它的 `TestConfigureSelfProxySkipsOwnSOCKSListener` 一并删除，改为断言 `ensureControlProxyDirect()` 之后 `tshttpproxy.ProxyFromEnvironment` 对控制面 URL 返回 nil。
- 新增 `controlLogf`：作为 `tsnet.Server.Logf`，只保留形如 `control:` 且含 `failed` 或 `error` 的最后一条信息（截断 200 字符，互斥量保护）；节点重建时清空。
- `statusFromState` 保留 `Health`；`Status()` 在非 `running` 状态下把 `Error` 填成「最后一条控制面错误」；`running` 时清空该字段。
  - 与初稿的偏差：不再用 `Health` 做兜底。暖机阶段 `Health` 是「Tailscale is starting. Please wait.」，把它拼在「错误:」后面会误导；控制面失联的 `Health` 要 8 分钟才出现，那时日志里已有更具体的错误。

前端（`app/src/config/tabs/tailscaleStatus.ts`）：

- `describeTailscaleStatus` 在非 `running`、且 `error` 非空时，把 `${languages.tailscaleError}: ${error}` 作为第二段追加到文案末尾（与现有等待提示并列，复用已有键，不新增 i18n）。
- `running` 状态依旧不显示任何错误，`loginURL` 规则不变。

测试：

- `kernel/tailscale/embed_test.go`：控制面直连生效；`controlLogf` 只留匹配行且截断；`Status()` 在无节点失败时带出最近控制面错误。
- `app/src/config/tabs/tailscaleStatus.test.ts`：`starting` + 无 URL + 有 error 时文案含等待提示与错误两段；`running` + 有 error 时不显示错误。

## 留给实现者的空间

- 错误文案的截断长度与拼接顺序可微调，语义不变。
- 是否在同一处把 `Health` 与日志错误都拼上，或二选一，由实现者按可读性定。