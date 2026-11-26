# 内嵌 Tailscale 未登录时给出可点登录链接 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 桌面版启用内嵌 Tailscale 后，只要节点还没连上 Tailnet 且内核拿得到认证链接，设置界面就必须渲染可点击的登录链接。

**Architecture:** 把「状态文案 + 是否给链接」的判断从 `appTab.ts` 里抽成纯函数模块，由前端测试锁死规则；后端补齐三条会丢掉链接的路径（含 peers 的 status 查询超时、节点尚未建立、`Stopped` 被当错误），并用一个包级缓存保证查询失败时链接不消失。

**Tech Stack:** TypeScript（原生 `node:test` + `transpileModule` + `runInNewContext`）、Go（`tailscale.com/tsnet`、`tailscale.com/client/local`）。

**Spec:** `docs/superpowers/specs/2026-10-30-tailscale-login-link-design.md`

## Global Constraints

- 不编译内核、不重启内核；Go 改动后只跑 `gofmt` 和 `go test`。
- 前端改完只跑 `cd app && pnpm run lint`，禁止跑 `pnpm build` / `pnpm dev`。
- i18n 新键加在每个 `app/appearance/langs/*.json` 对象的**最顶部**，22 个语言文件各自翻译，禁止照抄英文；改完跑 `python scripts/check-lang-keys.py`。
- 注释用中文，按 120 字符换行；描述代码做什么，不描述替换了什么。
- 本仓库规则禁止未经用户明确要求执行 `git commit`：各任务最后的提交步骤必须先取得用户同意。
- 登录链接一律 `<a class="fn__a" target="_blank" rel="noreferrer">`，复用 `tailscaleOpenLogin` 文案；Electron 侧已由 `setWindowOpenHandler` 交给 `shell.openExternal`。

## Review Focus

- 认证成功后后端仍残留旧 `authURL` → 界面不该再显示链接；`running` 必须不产生 `loginURL`，且成功查询拿到空值要清缓存（Task 1 / Task 5 的测试）。
- 状态查询失败或超时 → 链接不能消失；错误分支要带出缓存值（Task 5 的测试）。
- 配置已启用但节点还没建立 → 状态不能显示「未启用」（Task 6 的测试）。
- 控制面到不了、始终拿不到链接 → 不能只留一句「正在连接」，要有可行动的等待提示；反过来，端口占用这类真实故障不能被这句提示盖住（Task 1 的测试）。
- 每 3 秒轮询一次 → 链接节点不能越堆越多导致闪烁或点不中（Task 1 的幂等测试）。

---

### Task 1: 前端状态视图纯函数

**Files:**
- Create: `app/src/config/tabs/tailscaleStatus.ts`
- Test: `app/src/config/tabs/tailscaleStatus.test.ts`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `interface ITailscaleStatus { state: string; authURL: string; ips: string[] | null; error: string }`
  - `interface ITailscaleStatusView { text: string; loginURL: string }`
  - `interface ITailscaleLanguages { tailscaleRunning: string; tailscaleNeedsLogin: string; tailscaleStarting: string; tailscaleError: string; tailscaleDisabled: string; tailscaleWaitingAuth: string; tailscaleOpenLogin: string }`
  - `describeTailscaleStatus(status: ITailscaleStatus, languages: ITailscaleLanguages): ITailscaleStatusView`
  - `applyTailscaleStatus(element: HTMLElement, status: ITailscaleStatus, languages: ITailscaleLanguages): void`

- [ ] **Step 1: 写失败测试**

创建 `app/src/config/tabs/tailscaleStatus.test.ts`，沿用 `compactMode.test.ts` 的 `transpileModule` + `runInNewContext` 载入方式（模块无 import，只需注入 `document` 与 `exports`）：

```ts
import * as assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {ModuleKind, transpileModule} from "typescript";

const languages = {
    tailscaleRunning: "已连接", tailscaleNeedsLogin: "需要登录", tailscaleStarting: "正在连接",
    tailscaleError: "错误", tailscaleDisabled: "未启用",
    tailscaleWaitingAuth: "正在等待登录链接，请检查网络或代理", tailscaleOpenLogin: "打开登录链接",
};
const status = (state: string, authURL = "", error = "") => ({state, authURL, ips: null, error});

const load = () => {
    const exports = {} as typeof import("./tailscaleStatus");
    const document = {
        createElement: (tag: string) => ({
            tagName: tag.toUpperCase(), className: "", href: "", target: "", rel: "", textContent: "",
        }),
        createTextNode: (text: string) => ({nodeValue: text}),
    };
    runInNewContext(transpileModule(readFileSync("src/config/tabs/tailscaleStatus.ts", "utf8"), {
        compilerOptions: {module: ModuleKind.CommonJS},
    }).outputText, {exports, document});
    return exports;
};

test("未连接且有认证链接时给出链接", () => {
    const url = "https://login.tailscale.com/a/1";
    const starting = load().describeTailscaleStatus(status("starting", url), languages);
    assert.equal(starting.text, "正在连接");
    assert.equal(starting.loginURL, url);
    const needsLogin = load().describeTailscaleStatus(status("needsLogin", url), languages);
    assert.equal(needsLogin.text, "需要登录");
    assert.equal(needsLogin.loginURL, url);
});

test("已连接时不给出链接，哪怕后端残留 authURL", () => {
    const ret = load().describeTailscaleStatus(status("running", "https://login.tailscale.com/a/2"), languages);
    assert.equal(ret.text, "已连接");
    assert.equal(ret.loginURL, "");
});

test("已连接时把 Tailnet 地址附在文案后", () => {
    const ret = load().describeTailscaleStatus({state: "running", authURL: "", ips: ["100.64.0.5"], error: ""}, languages);
    assert.equal(ret.text, "已连接 · 100.64.0.5");
});

test("拿不到链接时给出等待提示", () => {
    assert.equal(load().describeTailscaleStatus(status("starting"), languages).text,
        "正在连接 · 正在等待登录链接，请检查网络或代理");
    assert.equal(load().describeTailscaleStatus(status("needsLogin"), languages).text,
        "需要登录 · 正在等待登录链接，请检查网络或代理");
});

test("错误与未启用状态不叠加等待提示", () => {
    assert.equal(load().describeTailscaleStatus(status("error", "", "listen failed"), languages).text,
        "错误: listen failed");
    assert.equal(load().describeTailscaleStatus(status("disabled"), languages).text, "未启用");
});

test("applyTailscaleStatus 渲染链接且重复调用不累积", () => {
    const api = load();
    const element = {
        _text: "", children: [] as unknown[],
        get textContent() { return this._text; },
        set textContent(value: string) { this._text = value; this.children.length = 0; },
        appendChild(child: unknown) { this.children.push(child); return child; },
    };
    const url = "https://login.tailscale.com/a/3";
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("needsLogin", url), languages);
    api.applyTailscaleStatus(element as unknown as HTMLElement, status("needsLogin", url), languages);
    assert.equal(element.textContent, "需要登录");
    assert.equal(element.children.length, 2);
    const link = element.children[1] as {tagName: string; className: string; href: string; target: string; rel: string};
    assert.deepEqual([link.tagName, link.className, link.href, link.target, link.rel],
        ["A", "fn__a", url, "_blank", "noreferrer"]);
    assert.equal((link as unknown as {textContent: string}).textContent, "打开登录链接");
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd app && node --import tsx --test src/config/tabs/tailscaleStatus.test.ts`
Expected: FAIL，`Cannot find module 'src/config/tabs/tailscaleStatus.ts'`（ENOENT）。

- [ ] **Step 3: 实现 `app/src/config/tabs/tailscaleStatus.ts`**

导出上列接口与两个函数。规则（与测试一一对应）：`running` 的 `loginURL` 恒为空且文案后缀拼接 `ips`；其余状态 `authURL` 非空即为 `loginURL`；`starting`/`needsLogin` 且 `authURL` 为空时文案追加 `" · " + tailscaleWaitingAuth`；`error` 有 `error` 文本时拼 `错误: <error>`；其他走 `tailscaleDisabled`。`applyTailscaleStatus` 先 `element.textContent = view.text`，`loginURL` 非空时再创建一个 `<a>`（`className="fn__a"`、`target="_blank"`、`rel="noreferrer"`、`textContent` 用 `tailscaleOpenLogin`）并追加一个 `" "` 文本节点。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd app && node --import tsx --test src/config/tabs/tailscaleStatus.test.ts`
Expected: PASS，7 个用例全绿。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add app/src/config/tabs/tailscaleStatus.ts app/src/config/tabs/tailscaleStatus.test.ts
git commit -m ":sparkles: Always offer the Tailscale login link while offline"
```

---

### Task 2: 设置界面接入状态视图

**Files:**
- Modify: `app/src/config/tabs/appTab.ts`（`interface ITailscaleStatus`、`mountTailscale` 内的 `renderStatus`）

**Interfaces:**
- Consumes: Task 1 的 `ITailscaleStatus`、`ITailscaleLanguages`、`applyTailscaleStatus`。
- Produces: 无（只改接线）。

- [ ] **Step 1: 删掉本地重复接口并改用纯函数**

删除 `appTab.ts` 内的 `interface ITailscaleStatus` 声明，改为从 `./tailscaleStatus` 导入 `applyTailscaleStatus`、`ITailscaleStatus`、`ITailscaleLanguages`；`ITailscaleData` 的 `status` 字段沿用导入的类型。

- [ ] **Step 2: 把 `renderStatus` 换成两行接线**

`mountTailscale` 内原有的 30 行 `switch` 全部删除，函数体改为调用 `applyTailscaleStatus(statusElement, status, window.siyuan.languages as ITailscaleLanguages)`。其余逻辑（`applyData`、`refresh`、确认按钮、3 秒轮询）不动。

- [ ] **Step 3: 类型检查与 lint**

Run: `cd app && pnpm run lint`
Expected: 通过，无 `ITailscaleStatus` 重复声明或未使用导入的报错。

- [ ] **Step 4: 提交（需用户明确同意后执行）**

```bash
git add app/src/config/tabs/appTab.ts
git commit -m ":recycle: Render the Tailscale login link from one status view"
```

---

### Task 3: 新增等待提示文案

**Files:**
- Modify: `app/appearance/langs/*.json`（22 个文件，各在对象最顶部插入 `tailscaleWaitingAuth`）

**Interfaces:**
- Consumes: Task 1 的 `ITailscaleLanguages.tailscaleWaitingAuth`。
- Produces: 运行时 `window.siyuan.languages.tailscaleWaitingAuth`。

- [ ] **Step 1: 在 `en.json` 与 `zh-CN.json`、`zh-TW.json` 写入定稿文案**

`en.json`: `"tailscaleWaitingAuth": "Waiting for the login link, check your network or proxy"`
`zh-CN.json`: `"tailscaleWaitingAuth": "正在等待登录链接，请检查网络或代理"`
`zh-TW.json`: `"tailscaleWaitingAuth": "正在等待登入連結，請檢查網路或代理"`

- [ ] **Step 2: 补齐其余 19 个语言文件**

`ar de es fr he hi id it ja ko nl pl pt-BR ru sk sr th tr uk` 各自翻译同一语义，插入位置同为对象顶部（`"tailscale"` 之前），使用三个 ASCII 句点，不得照抄英文。

- [ ] **Step 3: 校验键完整性**

Run: `python scripts/check-lang-keys.py`
Expected: 通过，无缺失键或多余键提示。

- [ ] **Step 4: 提交（需用户明确同意后执行）**

```bash
git add app/appearance/langs
git commit -m ":globe_with_meridians: Add the Tailscale login waiting hint"
```

---

### Task 4: 未启动状态不再当错误

**Files:**
- Modify: `kernel/tailscale/embed.go`（`statusFromState`）
- Test: `kernel/tailscale/embed_test.go`

**Interfaces:**
- Consumes: 无。
- Produces: `statusFromState(*ipnstate.Status) NodeStatus` 的新映射：`Running`→`running`，`NeedsLogin`/`NeedsMachineAuth`→`needsLogin`，`NoState`/`Starting`/`Stopped`→`starting`，其余→`error`。

- [ ] **Step 1: 写失败测试**

在 `embed_test.go` 的 `TestStatusFromState` 后追加：

```go
func TestStatusFromStateTreatsStoppedAsStarting(t *testing.T) {
	for _, state := range []string{"NoState", "Starting", "Stopped"} {
		if got := statusFromState(&ipnstate.Status{BackendState: state}); "starting" != got.State {
			t.Fatalf("backend state [%s] should be starting, got [%s]", state, got.State)
		}
	}
	if got := statusFromState(&ipnstate.Status{BackendState: "Stopped", AuthURL: "https://login.tailscale.com/a/1"}); "https://login.tailscale.com/a/1" != got.AuthURL {
		t.Fatalf("auth url should survive, got [%s]", got.AuthURL)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./tailscale/ -run 'TestStatusFromStateTreatsStoppedAsStarting' -v`
Expected: FAIL，`backend state [Stopped] should be starting, got [error]`。

- [ ] **Step 3: 改映射**

`statusFromState` 里把 `case "Starting", "NoState":` 改成 `case "Starting", "NoState", "Stopped":`，其余分支不动。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && go test ./tailscale/ -run 'TestStatusFromState' -v`
Expected: PASS，含既有 `TestStatusFromState` 与新增用例。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
gofmt -l kernel/tailscale
git add kernel/tailscale/embed.go kernel/tailscale/embed_test.go
git commit -m ":bug: Stop reporting a stopped Tailscale backend as an error"
```

---

### Task 5: 状态查询更快、失败也不丢登录链接

**Files:**
- Modify: `kernel/tailscale/embed.go`（`statusQueryTimeout`、`Status`、`Apply`、新增缓存函数）
- Test: `kernel/tailscale/embed_test.go`

**Interfaces:**
- Consumes: Task 4 的 `statusFromState`。
- Produces:
  - `rememberAuthURL(url string)`（内部，包级缓存写入，空串即清空）
  - `cachedAuthURL() string`
  - `Status() NodeStatus` 的所有失败分支都带上 `AuthURL: cachedAuthURL()`

- [ ] **Step 1: 写失败测试**

追加到 `embed_test.go`：

```go
func TestAuthURLCacheSurvivesQueryFailure(t *testing.T) {
	defer func() {
		rememberAuthURL("")
		nodeMu.Lock()
		lastErr = ""
		nodeMu.Unlock()
	}()

	rememberAuthURL("https://login.tailscale.com/a/9")
	if "https://login.tailscale.com/a/9" != cachedAuthURL() {
		t.Fatalf("cache should keep the auth url, got [%s]", cachedAuthURL())
	}

	setLastError(errors.New("query failed"))
	failed := Status()
	if "error" != failed.State || "https://login.tailscale.com/a/9" != failed.AuthURL {
		t.Fatalf("error status should still carry the cached auth url, got %#v", failed)
	}

	rememberAuthURL("")
	if "" != cachedAuthURL() {
		t.Fatalf("empty url should clear the cache, got [%s]", cachedAuthURL())
	}
}
```

说明：测试进程里没有可用节点，用 `setLastError` 制造出「查询失败」分支，验证错误状态能带出缓存。该用例需要给 `embed_test.go` 的 import 块补上 `errors`。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./tailscale/ -run 'TestAuthURLCacheSurvivesQueryFailure' -v`
Expected: FAIL，`undefined: rememberAuthURL`。

- [ ] **Step 3: 实现缓存与查询加固**

在 `var (...)` 块加 `authURLMu sync.Mutex` 与 `lastAuthURL string`；实现 `rememberAuthURL`/`cachedAuthURL`（加锁读写）。`statusQueryTimeout` 由 `2 * time.Second` 改为 `5 * time.Second`。`Status()` 里 `client.Status(ctx)` 换成 `client.StatusWithoutPeers(ctx)`；`LocalClient()` 失败、`StatusWithoutPeers` 失败、以及 `nil == node && "" != errMessage` 三个分支都补 `AuthURL: cachedAuthURL()`；成功查询后用 `rememberAuthURL(ret.AuthURL)` 写入（空串即清空）。`Apply()` 开头（`nodeMu` 置空 `current` 处）调一次 `rememberAuthURL("")`，避免禁用后残留旧链接。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && go test ./tailscale/ -v`
Expected: PASS，`kernel/tailscale` 全部用例通过。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
gofmt -l kernel/tailscale
git add kernel/tailscale/embed.go kernel/tailscale/embed_test.go
git commit -m ":bug: Keep the Tailscale login link when the status query fails"
```

---

### Task 6: 已启用但节点未建立时上报「正在连接」

**Files:**
- Modify: `kernel/tailscale/embed.go`（新增 `StatusFor`）
- Modify: `kernel/api/system.go`（`tailscaleData`）
- Test: `kernel/tailscale/embed_test.go`

**Interfaces:**
- Consumes: Task 5 的 `Status()`。
- Produces: `StatusFor(config *Config) NodeStatus`。

- [ ] **Step 1: 写失败测试**

追加到 `embed_test.go`：

```go
func TestStatusForEnabledWithoutNodeIsStarting(t *testing.T) {
	enabled := true
	if got := StatusFor(&Config{Enabled: &enabled}); "starting" != got.State {
		t.Fatalf("enabled config without node should be starting, got [%s]", got.State)
	}
	disabled := false
	if got := StatusFor(&Config{Enabled: &disabled}); "disabled" != got.State {
		t.Fatalf("disabled config should stay disabled, got [%s]", got.State)
	}
	if got := StatusFor(nil); "disabled" != got.State {
		t.Fatalf("nil config should stay disabled, got [%s]", got.State)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./tailscale/ -run 'TestStatusForEnabledWithoutNodeIsStarting' -v`
Expected: FAIL，`undefined: StatusFor`。

- [ ] **Step 3: 实现并接线**

`embed.go` 新增 `StatusFor(config *Config) NodeStatus`：先取 `Status()`，当 `nil != config && config.IsEnabled() && "disabled" == status.State` 时把状态改成 `starting` 再返回。`kernel/api/system.go` 的 `tailscaleData()` 里先把配置读出来，再用 `tailscale.StatusFor(config)` 取代 `tailscale.Status()`，配置解析失败时覆盖为 `error` 的既有逻辑保留。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && go test ./tailscale/ -v`
Expected: PASS。

- [ ] **Step 5: 全仓校验并提交（需用户明确同意后执行）**

```bash
gofmt -l kernel/tailscale kernel/api
cd kernel && go vet ./tailscale/ ./api/ && cd ..
git add kernel/tailscale/embed.go kernel/tailscale/embed_test.go kernel/api/system.go
git commit -m ":bug: Report an enabled Tailscale node as starting, not disabled"
```

说明：这里的校验只做类型检查与编译检查，不产出也不替换内核二进制。`go build ./...` 会在当前目录写出可执行文件，所以用 `go vet`。

---

## 完成后的手工验收（需要用户在自己的桌面版上做）

1. 关闭 Tailscale 登录态（或删掉 `~/.config/siyuan/tailscale/`），在「设置 → 应用」启用并确认。
2. 3 秒内应出现「正在连接」，并在链接就绪后出现「打开登录链接」。
3. 点链接能在系统浏览器打开 `login.tailscale.com`，完成认证后文案变为「已连接 · <Tailnet IP>」，链接消失。
4. 断网或配错代理时应显示「正在连接 · 正在等待登录链接，请检查网络或代理」，而不是空白。