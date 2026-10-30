# 内嵌 Tailscale 未登录时必须给出可点登录链接

## 意图

用户装了桌面版 3.8.9，在「设置 → 应用」里启用内嵌 Tailscale 并点确认后，节点处于未认证状态，界面上却没有任何登录链接，导致无法完成认证。用户要的是：**检测是否已经连上 Tailscale；没连上时，必须给出一个能点的认证链接。**

## 现状（已核实）

- 后端 `kernel/tailscale/embed.go::Status()` 每次查询都会把 `AuthURL` 带回来，`tailscaleData()` 也无条件透传。
- 前端 `app/src/config/tabs/appTab.ts::renderStatus()` 只在 `needsLogin` 分支渲染链接，`starting`/`error`/其他分支只写文字。
- tsnet 在 `NeedsLogin` **和** `NoState` 下都会持续打印登录链接（见 `tsnet.printAuthURLLoop`），说明「还在连接中」时链接往往已经存在。
- 现有三个把链接丢掉的路径：`current == nil` 时直接返回 `disabled`；`client.Status(ctx)` 含 peers 查询且 2 秒超时，失败即返回 `error` 并丢弃 URL；`Stopped` 等状态被映射成 `error`。
- 链接以 `<a target="_blank">` 渲染，Electron 侧 `setWindowOpenHandler` 会走 `shell.openExternal`，可唤起系统浏览器。

## 目标

1. 只要内嵌节点返回了 `authURL`，且节点未连接，界面就渲染可点链接，不依赖后端状态名。
2. 未连接且暂时没有 `authURL` 时，给出可行动提示（检查网络或代理），不留空白、不误报「未启用」。
3. 启动窗口内（配置已启用、节点尚未建立）状态显示为「正在连接」，不显示「未启用」。
4. `authURL` 不因单次查询失败或超时而丢失。
5. 登录成功后不再显示过期链接。

## 非目标

- 不新增自建控制服务器（Headscale `ControlURL`）配置项。
- 不做 WebSocket 主动推送登录提醒。
- 不改认证密钥流程、SOCKS5 监听地址、配置文件 `tailscale.json` 的结构。

## 反目标（功能跑通也算失败）

- 认证成功后仍挂着旧链接。
- 每 3 秒轮询都把链接节点拆了重建，造成闪烁或点不中。
- 内核启动就弹窗打扰用户。

## 约束

- Go 改动后 `gofmt`；按仓库规则不编译内核、不重启内核。
- 新增 i18n 键加在每个 `app/appearance/langs/*.json` 顶部并各自翻译，改完跑 `python scripts/check-lang-keys.py`。
- 前端改完只跑 `cd app && pnpm run lint`，不跑 `pnpm build` / `pnpm dev`。
- 注释用中文，按 120 字符换行。

## 方案

- **A 只改前端渲染**：链接绘制与状态名解耦。改动最小，但覆盖不到后端丢弃 URL 的路径（含 peers 查询超时是最常见的失败点）。
- **B 前端渲染规则 + 后端补全（选定）**：在 A 的基础上，后端起用 `StatusWithoutPeers`、放宽超时、缓存最近一次拿到的 `authURL`，并在「配置已启用但节点还没建立」时返回 `starting`。
- **C B + 推送通知**：`util.PushMsg` 只支持纯文本，要可点链接必须新增通知类型（内核 + 前端 + i18n），明显更重，且引入启动即弹窗的打扰风险。本次不做。

选 B 的理由：用户要的是「没连上就必须有链接」，而链接最容易被丢掉的正是查询失败和启动窗口这两个路径，只改前端补不上。

## 落实细节（已定）

前端：

- 新增纯函数模块 `app/src/config/tabs/tailscaleStatus.ts`，导出 `describeTailscaleStatus()`：输入 `state`、`authURL`、`ips`、`error` 和语言包，输出 `{text, loginURL}`。
- 规则：`running` 一律不返回 `loginURL`（即使后端残留 URL）；其余状态只要 `authURL` 非空就返回它；文案仍按 `state` 决定。
- `renderStatus()` 改为调用该函数：先写文本，`loginURL` 非空时追加 `<a class="fn__a" target="_blank" rel="noreferrer">`，沿用现有 `tailscaleOpenLogin` 文案。
- 新增 i18n 键 `tailscaleWaitingAuth`：未连接且无链接时的提示语。

后端：

- `statusFromState()` 把 `NoState`、`Starting`、`Stopped` 统一映射为 `starting`：这三种都表示节点还没起来、并未报告故障，不该按错误处理而丢掉链接。
- `Status()` 改用 `client.StatusWithoutPeers(ctx)`，`statusQueryTimeout` 由 2 秒放宽到 5 秒。
- 包级缓存最近一次成功查询得到的 `authURL`：成功查询时写入（空串即清空），查询失败时把缓存值带在 `error` 状态里返回。
- `kernel/api/system.go::tailscaleData()`：当 `config.IsEnabled()` 且 `status.State == "disabled"` 时，把状态改成 `starting`。

测试：

- `kernel/tailscale/embed_test.go`：扩展 `TestStatusFromState` 覆盖 `NoState`、`Stopped`；新增用例覆盖缓存的写入、空值清空、失败时保留。
- 新增 `app/src/config/tabs/tailscaleStatus.test.ts`：`state` × `authURL` 组合矩阵，重点锁住「`running` 不显示链接」「`starting` + URL 显示链接」「无 URL 时给出等待提示」。

## 留给实现者的空间

- 函数名、i18n 键名的最终措辞可调整，语义不变。
- 各语言提示语按本地表达习惯翻译，不要求逐字对应。
- 是否在缓存 `authURL` 时额外记一行内核日志，由实现者决定。