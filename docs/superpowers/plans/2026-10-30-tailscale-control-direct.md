# 控制面固定直连 + 失败原因可见 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 内嵌 Tailscale 的控制面连接不再受「设置 → 网络代理」影响，且连接失败的真实原因能显示在设置界面。

**Architecture:** 在 `Apply()` 最先用 `tshttpproxy.SetProxyFunc` 装一个恒返回 nil 的代理函数（`sync.Once` 保证只装一次），删掉 `configureSelfProxy`；再用 `tsnet.Server.Logf` 截留最后一条控制面错误，填进既有 `NodeStatus.Error`，前端在非 `running` 状态下追加显示。

**Tech Stack:** Go（`tailscale.com/tsnet`、`tailscale.com/net/tshttpproxy`）、TypeScript（原生 `node:test` + `transpileModule`）。

**Spec:** `docs/superpowers/specs/2026-10-30-tailscale-control-direct-design.md`

## Global Constraints

- 不编译内核二进制、不重启内核；Go 改动后 `gofmt` 并跑 `go test ./tailscale/`。
- 前端只跑 `cd app && pnpm run lint`，禁止 `pnpm build` / `pnpm dev`。
- 不新增 API 契约字段；不新增 i18n 键（复用 `tailscaleError`）。
- 注释中文、120 字符换行。

---

### Task 1: 控制面固定直连

**Files:**
- Modify: `kernel/tailscale/embed.go`
- Test: `kernel/tailscale/embed_test.go`

**Interfaces:**
- Produces: `ensureControlProxyDirect()`（内部函数，`sync.Once` 保护）
- 删除: `configureSelfProxy(listen string)`、`TestConfigureSelfProxySkipsOwnSOCKSListener`

- [ ] **Step 1: 写失败测试** — 断言调用 `ensureControlProxyDirect()` 之后，`tshttpproxy.ProxyFromEnvironment` 对 `https://controlplane.tailscale.com/key` 返回 nil 代理（先设置 `HTTP_PROXY`/`HTTPS_PROXY` 为某个地址以模拟旧配置）。
- [ ] **Step 2: 跑测试确认失败** — `cd kernel && go test ./tailscale/ -run TestEnsureControlProxyDirect -v`，预期 `undefined: ensureControlProxyDirect`。
- [ ] **Step 3: 实现** — 新增 `ensureControlProxyDirect()`；`Apply()` 开头调用；删除 `configureSelfProxy` 及其调用；删除对应旧测试。
- [ ] **Step 4: 验证** — `gofmt -l kernel/tailscale` 为空；`go test ./tailscale/` 全绿。

### Task 2: 控制面错误可见

**Files:**
- Modify: `kernel/tailscale/embed.go`、`app/src/config/tabs/tailscaleStatus.ts`
- Test: `kernel/tailscale/embed_test.go`、`app/src/config/tabs/tailscaleStatus.test.ts`

**Interfaces:**
- Produces: `controlLogf(format string, args ...any)`（`tsnet.Server.Logf`）、`lastControlError() string`、`resetControlError()`
- 沿用: `NodeStatus.Error`、`ITailscaleLanguages.tailscaleError`

- [ ] **Step 1: 写失败测试（Go）** — `controlLogf` 只保留含 `control:` 且含 `failed`/`error` 的最后一条，截断 200 字符；`resetControlError()` 后为空。
- [ ] **Step 2: 写失败测试（前端）** — `starting` + 无 URL + 有 error → 文案包含等待提示与 `错误: …` 两段；`running` + 有 error → 不显示错误。
- [ ] **Step 3: 实现（Go）** — `controlLogf`/`lastControlError`/`resetControlError`；`tsnet.Server` 加 `Logf: controlLogf`；`Apply()` 开头 `resetControlError()`；`Status()` 在非 `running` 且 `ret.Error` 为空时填 `lastControlError()`（不再用 Health 兜底：暖机文案不是错误）。
- [ ] **Step 4: 实现（前端）** — `describeTailscaleStatus` 在非 `running` 且 `error` 非空时追加 `" · " + tailscaleError + ": " + error`。
- [ ] **Step 5: 验证** — `go test ./tailscale/` 与 `node --import tsx --test src/config/tabs/tailscaleStatus.test.ts` 全绿；`pnpm run lint` 通过。

## 完成后的验收

1. 重编内核与前端、重新打包安装包。
2. 装包后：设置 → 应用 → Tailscale，把网络代理指向任意地址（如 `socks5://172.25.209.110:1055`）并保存 → 节点仍应能连上控制面并给出登录链接，无需重启内核。
3. 断开网络（拔网线/停代理）→ 界面应在若干秒内显示「错误: …」真实原因，而不是只有等待提示。