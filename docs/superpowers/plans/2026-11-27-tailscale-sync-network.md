# 内嵌 Tailscale 同步网络直连/代理与登录闭环 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** S3/WebDAV 同步可显式选择「直连」或「代理（内嵌 Tailscale 节点）」，其他内核流量保持直连；未登录/待批准时快速失败，登录后自动补同步。

**Architecture:** 在 `kernel/tailscale` 内增加状态缓存和 `tsnet.Dial` 传输层，S3/WebDAV 客户端按 `tailscale.json` 的 `syncNetwork` 选择直连或代理，不再依赖 127.0.0.1:1055 的 SOCKS；`kernel/model` 负责把「未登录/待批准」变成快速失败、单条常驻提示和登录后的自动补同步。

**Tech Stack:** Go（`tailscale.com/tsnet`、`net/http`、`aws-sdk-go-v2` 间接）、TypeScript（原生 `node:test` + `transpileModule`）。

**Spec:** `docs/superpowers/specs/2026-11-27-tailscale-sync-network-design.md`

## Global Constraints

- 不编译内核、不重启内核；Go 改动后跑 `gofmt -w` 和 `go test`。
- 前端只跑 `cd app && pnpm run lint`（含 typecheck），禁止 `pnpm build` / `pnpm dev`。
- i18n 新键加在每个 `app/appearance/langs/*.json` 对象的**最顶部**，23 个语言文件各自翻译，禁止照抄英文；改完跑 `python scripts/check-lang-keys.py`。
- API 契约先改 `kernel/apicontract`，再 `cd app && pnpm run api:generate` / `pnpm run api:check`；不手改生成文件。
- 注释中文、120 字符换行；描述当前行为，不写变更历史。
- 本仓库规则禁止未经用户明确要求执行 `git commit`：各任务最后的提交步骤必须先取得用户同意。
- 1055 SOCKS 监听保留给外部工具；SiYuan 自身的 S3/WebDAV 不再经过它。
- 现有测试夹具和公共辅助函数优先复用；不新增第三方依赖。
- 本任务使用主 `dev` checkout（`D:/WorkSpace/subsystem/ubuntu2004/home/source_code/siyuan`），已在 `worktree/tasks.json` 申报 `tailscale-sync-network-20261127`；如需隔离 worktree 再按仓库规则处理。

## Review Focus

- 未登录/待批准时拨号立即失败，不消耗 S3 的 60 秒超时；错误文案能区分未登录与待管理员批准（Task 3 / Task 6 的测试）。
- 登录完成（`running`）只触发一次补同步；成功或再次失败都只保留一条提示，不刷屏（Task 6 的测试）。
- 只改 `syncNetwork` 不重启节点；改其它字段仍会重建（Task 1 的测试）。
- 「直连」忽略全局网络代理，「代理」只走内嵌节点；真实代理设置仍对其它流量生效（Task 4 / Task 5 的测试）。
- 设置页 `running` 不显示旧登录链接；`needsMachineAuth` 与密钥过期显示正确（Task 9 的测试）。

---

### Task 1: `syncNetwork` 配置、模式读取与免重启保存

**Files:**
- Modify: `kernel/tailscale/config.go`
- Test: `kernel/tailscale/config_test.go`

**Interfaces:**
- Consumes: 现有 `Config`、`LoadConfig`、`SaveConfig`、`prepareConfig`。
- Produces:
  - `const SyncNetworkDirect = "direct"`、`const SyncNetworkProxy = "proxy"`
  - `Config.SyncNetwork string`（JSON `syncNetwork`）
  - `func SyncNetworkMode() string`（读 `ConfigDir()`，缺省 `direct`）
  - `func IsEmbeddedProxy(proxyURL string) bool`
  - 内部 `func syncNetworkMode(configDir string) string`
  - 内部 `func isEmbeddedProxy(configDir, proxyURL string) bool`
  - 内部 `func nodeConfigChanged(old, next *Config) bool`
  - 测试钩子 `var applyConfig = Apply`

- [ ] **Step 1: 写失败测试**

在 `kernel/tailscale/config_test.go` 增加（沿用现有 `writeConfig(t, ...)` 辅助函数）：

```go
func TestConfigNormalizeSyncNetwork(t *testing.T) {
	enabled := true
	disabled := false
	for _, test := range []struct {
		name    string
		enabled *bool
		want    string
	}{{"enabled-default-proxy", &enabled, SyncNetworkProxy}, {"disabled-default-direct", &disabled, SyncNetworkDirect}} {
		config := &Config{Enabled: test.enabled}
		config.normalize(t.TempDir())
		if config.SyncNetwork != test.want {
			t.Fatalf("%s: SyncNetwork = %q, want %q", test.name, config.SyncNetwork, test.want)
		}
	}
	config := &Config{Enabled: &enabled, SyncNetwork: SyncNetworkDirect}
	config.normalize(t.TempDir())
	if config.SyncNetwork != SyncNetworkDirect {
		t.Fatalf("explicit direct should be preserved, got %q", config.SyncNetwork)
	}
}

func TestConfigValidateSyncNetwork(t *testing.T) {
	if err := (&Config{Listen: "127.0.0.1:1055", SyncNetwork: "bogus"}).validate(); nil == err {
		t.Fatal("invalid syncNetwork should be rejected")
	}
}

func TestNodeConfigChangedIgnoresSyncNetwork(t *testing.T) {
	old := &Config{Enabled: boolPtr(true), Hostname: "a", Listen: "127.0.0.1:1055", StateDir: "s", SyncNetwork: SyncNetworkProxy}
	next := *old
	next.SyncNetwork = SyncNetworkDirect
	if nodeConfigChanged(old, &next) {
		t.Fatal("syncNetwork alone should not rebuild the node")
	}
	next.Hostname = "b"
	if !nodeConfigChanged(old, &next) {
		t.Fatal("hostname change should rebuild the node")
	}
}

func TestSaveConfigSkipsApplyForSyncNetworkOnly(t *testing.T) {
	configDir := t.TempDir()
	initial := &Config{Enabled: boolPtr(true), Hostname: "node", Listen: "127.0.0.1:2055", AuthKey: "key"}
	initial.normalize(configDir)
	if err := writeConfigFile(configDir, initial); nil != err {
		t.Fatalf("write initial config: %s", err)
	}
	applied := make(chan struct{}, 2)
	applyConfig = func(string) { applied <- struct{}{} }
	t.Cleanup(func() { applyConfig = Apply })

	if err := SaveConfig(configDir, &Config{Enabled: boolPtr(true), Hostname: "node", Listen: "127.0.0.1:2055", AuthKey: "", SyncNetwork: SyncNetworkDirect}); nil != err {
		t.Fatalf("save syncNetwork only: %s", err)
	}
	select {
	case <-applied:
		t.Fatal("syncNetwork only must not rebuild the node")
	case <-time.After(20 * time.Millisecond):
	}

	if err := SaveConfig(configDir, &Config{Enabled: boolPtr(true), Hostname: "renamed", Listen: "127.0.0.1:2055", AuthKey: "", SyncNetwork: SyncNetworkDirect}); nil != err {
		t.Fatalf("save hostname: %s", err)
	}
	select {
	case <-applied:
	case <-time.After(time.Second):
		t.Fatal("hostname change should rebuild the node")
	}
}

func TestIsEmbeddedProxy(t *testing.T) {
	configDir := t.TempDir()
	config := &Config{Enabled: boolPtr(true), Hostname: "node", Listen: "127.0.0.1:1055"}
	config.normalize(configDir)
	if err := writeConfigFile(configDir, config); nil != err {
		t.Fatalf("write config: %s", err)
	}
	for _, test := range []struct {
		proxy string
		want  bool
	}{
		{"socks5://127.0.0.1:1055", true},
		{"socks5://localhost:1055", true},
		{"http://127.0.0.1:1055", false},
		{"socks5://127.0.0.1:1080", false},
		{"", false},
	} {
		if got := isEmbeddedProxy(configDir, test.proxy); got != test.want {
			t.Fatalf("isEmbeddedProxy(%q) = %v, want %v", test.proxy, got, test.want)
		}
	}
}

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./tailscale/ -run 'TestConfigNormalizeSyncNetwork|TestConfigValidateSyncNetwork|TestNodeConfigChanged|TestSaveConfigSkipsApply' -v`
Expected: FAIL，`undefined: SyncNetworkDirect` / `undefined: nodeConfigChanged`。

- [ ] **Step 3: 实现**

- `Config` 加 `SyncNetwork string \`json:"syncNetwork"\``；`normalize` 中空值按 `IsEnabled()` 取 `proxy`/`direct`；`validate` 只接受 `direct`/`proxy`。
- `SaveConfig` 在写文件前 `old, _ := LoadConfig(configDir)`；`nodeConfigChanged(old, prepared)` 为 true 才 `go applyConfig(configDir)`。`nodeConfigChanged` 比较 `Enabled`（用 `IsEnabled()`，避免 nil/true 指针差异）、`Hostname`、`AuthKey`、`Listen`、`StateDir`，忽略 `SyncNetwork`。
- `syncNetworkMode(configDir)`：`LoadConfig` 失败或 nil → `direct`；否则 `config.SyncNetwork`（normalize 已保证非空）。`SyncNetworkMode()` 用 `ConfigDir()`。
- `isEmbeddedProxy(configDir, proxyURL)`：解析 URL，scheme 为 `socks5`/`socks5h`，host:port 与 `config.Listen` 相同（`localhost` 与 `127.0.0.1` 视为等价）；配置缺失或 `!IsEnabled()` → false。`IsEmbeddedProxy()` 用 `ConfigDir()`。
- `applyConfig` 变量声明为 `var applyConfig = Apply`，`SaveConfig` 改为 `go applyConfig(configDir)`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && gofmt -w tailscale/config.go tailscale/config_test.go && go test ./tailscale/ -run 'TestConfig|TestNodeConfigChanged|TestSaveConfig|TestIsEmbeddedProxy' -v`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add kernel/tailscale/config.go kernel/tailscale/config_test.go
git commit -m ":sparkles: Add a sync network mode to the embedded Tailscale config"
```

---

### Task 2: 节点状态缓存、`needsMachineAuth` 与密钥到期

**Files:**
- Modify: `kernel/tailscale/embed.go`
- Create: `kernel/tailscale/state.go`（`//go:build !android && !ios`）
- Create: `kernel/tailscale/state_mobile.go`（`//go:build android || ios`，空实现）
- Test: `kernel/tailscale/embed_test.go`, `kernel/tailscale/state_test.go`

**Interfaces:**
- Produces:
  - `NodeStatus{State, AuthURL string; IPs []string; Error string; KeyExpiry string}`（`KeyExpiry` 为 RFC3339，未知为空）
  - `func statusFromState(status *ipnstate.Status) NodeStatus`（新增 `needsMachineAuth`、`KeyExpiry`）
  - `type stateCache`（`update`、`snapshot`、`isBlocked`、`setPendingRetry`、`consumePendingRetry`、`setOnRunning`；`setOnRunning` 替换而非追加）
  - 桌面导出包装：`SetOnRunning(func())`、`IsBlocked() bool`、`SetPendingRetry()`、`ConsumePendingRetry() bool`、`SyncBlockedMessage() string`
  - 移动端 `state_mobile.go` 提供同名空实现（`IsBlocked` 恒 false、`ConsumePendingRetry` 恒 false、`SyncBlockedMessage` 返回空串）
  - 包级 `var nodeState stateCache`（桌面）
  - 内部 `func watchNode(server *tsnet.Server)`

- [ ] **Step 1: 写失败测试**

`kernel/tailscale/state_test.go`：

```go
func TestStateCacheFiresOnRunningOnce(t *testing.T) {
	var cache stateCache
	fired := 0
	cache.setOnRunning(func() { fired++ })
	if cache.update(NodeStatus{State: "starting"}) {
		t.Fatal("starting should not be a running transition")
	}
	if !cache.update(NodeStatus{State: "running"}) {
		t.Fatal("first running update should be a transition")
	}
	if cache.update(NodeStatus{State: "running"}) {
		t.Fatal("staying running should not be a transition")
	}
	if 1 != fired {
		t.Fatalf("callbacks = %d, want 1", fired)
	}
}

func TestStateCacheBlockedAndPendingRetry(t *testing.T) {
	var cache stateCache
	for _, state := range []string{"needsLogin", "needsMachineAuth"} {
		cache.update(NodeStatus{State: state})
		if !cache.isBlocked() {
			t.Fatalf("%s should be blocked", state)
		}
	}
	cache.update(NodeStatus{State: "running"})
	if cache.isBlocked() {
		t.Fatal("running should not be blocked")
	}
	cache.setPendingRetry()
	if !cache.consumePendingRetry() {
		t.Fatal("pending retry should be consumed once")
	}
	if cache.consumePendingRetry() {
		t.Fatal("pending retry should be cleared after consumption")
	}
}

func TestSyncBlockedMessage(t *testing.T) {
	if "" == SyncBlockedMessage() {
		t.Fatal("blocked message should not be empty even without language files")
	}
}
```

`embed_test.go` 扩展 `TestStatusFromState`：

```go
needsAuth := statusFromState(&ipnstate.Status{BackendState: "NeedsMachineAuth"})
if "needsMachineAuth" != needsAuth.State {
	t.Fatalf("NeedsMachineAuth state = %q, want needsMachineAuth", needsAuth.State)
}
expiry := time.Now().Add(24 * time.Hour).UTC()
keyExpiring := statusFromState(&ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{KeyExpiry: &expiry}})
if expiry.Format(time.RFC3339) != keyExpiring.KeyExpiry {
	t.Fatalf("KeyExpiry = %q, want %q", keyExpiring.KeyExpiry, expiry.Format(time.RFC3339))
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./tailscale/ -run 'TestStateCache|TestStatusFromState' -v`
Expected: FAIL，`undefined: stateCache` / `KeyExpiry` 字段不存在。

- [ ] **Step 3: 实现**

- `state.go`（`//go:build !android && !ios`）：`stateCache` 用 `sync.Mutex` 保护 `status NodeStatus`、`ready bool`、`pendingRetry bool`、`onRunning []func()`；`update` 在锁内比较旧/新 `State`，首次进入 `running` 时复制回调，解锁后依次执行；`resetStatus` 清空状态与 `ready`，保留回调和 `pendingRetry`；`setOnRunning` 替换回调而不是追加。再提供 `SetOnRunning`/`IsBlocked`/`SetPendingRetry`/`ConsumePendingRetry` 导出包装和 `SyncBlockedMessage()`。
- `state_mobile.go`（`//go:build android || ios`）：`SetOnRunning` 空实现、`IsBlocked`/`ConsumePendingRetry` 返回 false、`SetPendingRetry` 空实现、`SyncBlockedMessage` 返回空串，保证 `kernel/model` 在移动端可以编译。
- `statusFromState`：`NeedsMachineAuth` → `needsMachineAuth`；`Self.KeyExpiry` 非 nil 时格式化为 RFC3339。
- `embed.go`：`Apply` 在 `server.Start()` 之后 `go watchNode(server)`；`watchNode` 每 2 秒（`running` 时 10 秒）用 `server.LocalClient().StatusWithoutPeers(ctx)` 查询，成功后 `nodeState.update(...)`，节点被替换时退出。`Status()` 查询成功后也 `nodeState.update(...)`，保证没有 watcher 时（例如测试或旧节点）缓存仍可用。
- `SyncBlockedMessage()`：`localized("tailscaleSyncBlocked")`，缺语言包时回退英文 `"Embedded Tailscale is not logged in; sync resumes after login"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && gofmt -w tailscale/embed.go tailscale/state.go tailscale/state_mobile.go tailscale/embed_test.go tailscale/state_test.go && go test ./tailscale/ -run 'TestStatusFromState|TestStateCache|TestSyncBlockedMessage' -v`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add kernel/tailscale/embed.go kernel/tailscale/state.go kernel/tailscale/state_mobile.go kernel/tailscale/embed_test.go kernel/tailscale/state_test.go
git commit -m ":sparkles: Track embedded Tailscale login state and key expiry"
```

---

### Task 3: `tsnet.Dial` 传输层与快速失败

**Files:**
- Create: `kernel/tailscale/transport.go`（无 build tag：错误哨兵、`SyncTransport` 类型）
- Create: `kernel/tailscale/transport_desktop.go`（`//go:build !android && !ios`：`DialContext`、`NewSyncTransport`）
- Create: `kernel/tailscale/transport_mobile.go`（`//go:build android || ios`：移动端空实现）
- Test: `kernel/tailscale/transport_test.go`

**Interfaces:**
- Consumes: `nodeState`、`current`、`nodeEnabled`、`SyncNetworkMode()`（Task 1/2）。
- Produces:
  - `var ErrDisabled/ErrNeedsLogin/ErrNeedsMachineAuth/ErrNotReady error`（`transport.go`）
  - `func DialContext(ctx context.Context, network, address string) (net.Conn, error)`
  - `type SyncTransport struct { *http.Transport; mode string }`
  - `func (t *SyncTransport) Mode() string`
  - `func NewSyncTransport(skipTLSVerify bool) *SyncTransport`
  - 测试钩子 `var dialNode = func(node *runningNode, ctx context.Context, network, address string) (net.Conn, error)`
  - 包级 `var nodeEnabled atomic.Bool`（`Apply` 设置）
  - 移动端 `transport_mobile.go`：`DialContext` 返回 `ErrDisabled`；`NewSyncTransport` 返回带 `ProxyFromEnvironment` 的旧 transport（移动端没有内嵌节点，保持原有代理行为）

- [ ] **Step 1: 写失败测试**

```go
func TestDialContextFastFailsWhenBlocked(t *testing.T) {
	for _, test := range []struct {
		state NodeStatus
		want  error
	}{{NodeStatus{State: "needsLogin"}, ErrNeedsLogin}, {NodeStatus{State: "needsMachineAuth"}, ErrNeedsMachineAuth}} {
		nodeState.resetStatus()
		nodeState.update(test.state)
		nodeEnabled.Store(true)
		current = &runningNode{}
		called := false
		dialNode = func(*runningNode, context.Context, string, string) (net.Conn, error) {
			called = true
			return nil, errors.New("should not dial")
		}
		_, err := DialContext(context.Background(), "tcp", "100.94.63.128:9000")
		if !errors.Is(err, test.want) {
			t.Fatalf("error = %v, want %v", err, test.want)
		}
		if called {
			t.Fatal("blocked state must not start a tsnet dial")
		}
		if !nodeState.consumePendingRetry() {
			t.Fatal("blocked dial should request a retry")
		}
	}
}

func TestDialContextRetriesNoState(t *testing.T) {
	nodeState.resetStatus()
	nodeState.update(NodeStatus{State: "starting"})
	nodeEnabled.Store(true)
	current = &runningNode{}
	attempts := 0
	noStateRetryDelay = time.Millisecond
	dialNode = func(*runningNode, context.Context, string, string) (net.Conn, error) {
		attempts++
		if attempts < 2 {
			return nil, errors.New("tsnet: backend in state NoState")
		}
		server, client := net.Pipe()
		go client.Close()
		return server, nil
	}
	conn, err := DialContext(context.Background(), "tcp", "100.94.63.128:9000")
	if nil != err || nil == conn {
		t.Fatalf("dial after NoState retry: %v", err)
	}
	if 2 != attempts {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestNewSyncTransportModes(t *testing.T) {
	oldHome := util.HomeDir
	util.HomeDir = t.TempDir()
	t.Cleanup(func() { util.HomeDir = oldHome })
	for _, test := range []struct {
		mode string
		want string
	}{{SyncNetworkDirect, "direct"}, {SyncNetworkProxy, "proxy"}} {
		config := &Config{Enabled: boolPtr(true), Hostname: "node", Listen: "127.0.0.1:2055", SyncNetwork: test.mode}
		config.normalize(ConfigDir())
		if err := writeConfigFile(ConfigDir(), config); nil != err {
			t.Fatalf("write config: %s", err)
		}
		transport := NewSyncTransport(false)
		if transport.Mode() != test.want {
			t.Fatalf("Mode() = %q, want %q", transport.Mode(), test.want)
		}
		if nil != transport.Proxy {
			t.Fatal("sync transport must ignore the environment proxy")
		}
	}
}
```

> 注：`ConfigDir()` 使用 `util.HomeDir`；测试前后保存/恢复该全局变量，且不要并行运行。
>
> 注：`TestDialContext*` 会修改包级 `current`、`nodeEnabled`、`dialNode`、`noStateRetryDelay`；每个用例用 `t.Cleanup` 恢复，不要并行运行。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./tailscale/ -run 'TestDialContextFastFails|TestDialContextRetriesNoState|TestNewSyncTransportModes' -v`
Expected: FAIL，`undefined: DialContext` / `ErrNeedsLogin`。

- [ ] **Step 3: 实现**

- `transport.go`（无 build tag）：错误哨兵 `ErrDisabled`/`ErrNeedsLogin`/`ErrNeedsMachineAuth`/`ErrNotReady`；`type stateError struct{ msg string; kind error }`（`Error()` 返回本地化文案、`Unwrap()` 返回 `kind`，让 `errors.Is` 可用）；`type SyncTransport struct{ *http.Transport; mode string }` 与 `Mode()`。
- `transport_desktop.go`（`//go:build !android && !ios`）：
  - `DialContext`：`current == nil` 时 `!nodeEnabled.Load()` → `ErrDisabled`，否则 `ErrNotReady`，两者都 `nodeState.setPendingRetry()`；缓存状态 `needsLogin`/`needsMachineAuth` → 对应错误并置 `pendingRetry`；否则循环调用 `dialNode(current, ctx, network, address)`，错误含 `backend in state NoState` 且未超过 2 次重试时 `time.Sleep(noStateRetryDelay)`（包级变量，默认 200ms）后重试，仍失败返回 `ErrNotReady` 并置 `pendingRetry`。
  - `NewSyncTransport`：`base := httpclient.NewTransport(skipTLSVerify)`；`base.Proxy = nil`；`proxy` 模式 `base.DialContext = DialContext`；`direct` 模式保留 `base.DialContext`；返回 `&SyncTransport{Transport: base, mode: mode}`。
  - `var dialNode = func(node *runningNode, ...)` 测试钩子与 `var nodeEnabled atomic.Bool`。
- `transport_mobile.go`（`//go:build android || ios`）：`DialContext` 返回 `ErrDisabled`；`NewSyncTransport` 返回 `&SyncTransport{Transport: httpclient.NewTransport(skipTLSVerify), mode: SyncNetworkDirect}`（保留 `ProxyFromEnvironment`，移动端行为不变）。
- `embed.go` 的 `Apply`：加载配置后 `nodeEnabled.Store(nil != config && config.IsEnabled())`；节点停止/禁用时 `nodeEnabled.Store(false)`。
- 不把 `pendingRetry` 设置到普通网络错误上（只有 blocked/未就绪/NoState）。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && gofmt -w tailscale/transport.go tailscale/transport_desktop.go tailscale/transport_mobile.go tailscale/embed.go tailscale/transport_test.go && go test ./tailscale/ -run 'TestDialContext|TestNewSyncTransport' -v`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add kernel/tailscale/transport.go kernel/tailscale/transport_desktop.go kernel/tailscale/transport_mobile.go kernel/tailscale/embed.go kernel/tailscale/transport_test.go
git commit -m ":sparkles: Dial the tailnet directly for sync traffic"
```

---

### Task 4: 接入 S3、WebDAV 与测试连接

**Files:**
- Modify: `kernel/model/repository.go:3276-3288`
- Modify: `kernel/model/sync_probe.go:91`
- Test: `kernel/model/sync_probe_test.go`（新增用例）

**Interfaces:**
- Consumes: `tailscale.NewSyncTransport(skipTLSVerify bool) *tailscale.SyncTransport`。
- Produces: 无新导出符号；S3/WebDAV 的 `http.Client`/`gowebdav` transport 换成 `NewSyncTransport`。
- 移动端由 `transport_mobile.go` 提供同一函数，保留原有 `ProxyFromEnvironment` 行为。

- [ ] **Step 1: 写失败测试**

在 `kernel/model/sync_probe_test.go` 增加：

```go
func TestS3ProbeUsesSyncTransportMode(t *testing.T) {
	oldHome := util.HomeDir
	util.HomeDir = t.TempDir()
	t.Cleanup(func() { util.HomeDir = oldHome })
	configDir := tailscale.ConfigDir()
	if err := os.MkdirAll(configDir, 0755); nil != err {
		t.Fatalf("create config dir: %s", err)
	}
	write := func(mode string) {
		payload := `{"enabled":true,"listen":"127.0.0.1:2055","syncNetwork":"` + mode + `"}`
		if err := os.WriteFile(filepath.Join(configDir, "tailscale.json"), []byte(payload), 0600); nil != err {
			t.Fatalf("write config: %s", err)
		}
	}
	for _, test := range []struct{ mode, want string }{{"direct", "direct"}, {"proxy", "proxy"}} {
		write(test.mode)
		client := newS3ProbeClient(&conf.S3{Endpoint: "http://s3.example", Timeout: 60, ConcurrentReqs: 1})
		s3, ok := client.(*cloud.S3)
		if !ok {
			t.Fatalf("probe client type = %T", client)
		}
		ua, ok := s3.HTTPClient.Transport.(*httpclient.UserAgentTransport)
		if !ok {
			t.Fatalf("transport type = %T", s3.HTTPClient.Transport)
		}
		transport, ok := ua.Base.(*tailscale.SyncTransport)
		if !ok {
			t.Fatalf("base transport type = %T", ua.Base)
		}
		if transport.Mode() != test.want {
			t.Fatalf("mode = %q, want %q", transport.Mode(), test.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./model -run TestS3ProbeUsesSyncTransportMode -v`
Expected: FAIL，`base transport type = *http.Transport`。

- [ ] **Step 3: 实现**

- `repository.go`：S3 用 `httpclient.NewUserAgentClient(tailscale.NewSyncTransport(cloudConf.S3.SkipTlsVerify))`；WebDAV 用 `webdavClient.SetTransport(tailscale.NewSyncTransport(cloudConf.WebDAV.SkipTlsVerify))`。
- `sync_probe.go`：`newS3ProbeClient` 同样用 `tailscale.NewSyncTransport(s3.SkipTlsVerify)`。
- 导入 `kernel/tailscale`；确认无循环依赖（`tailscale` 只依赖 `kernel/util`）。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && gofmt -w model/repository.go model/sync_probe.go model/sync_probe_test.go && go test ./model -run 'TestS3Probe' -v`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add kernel/model/repository.go kernel/model/sync_probe.go kernel/model/sync_probe_test.go
git commit -m ":recycle: Route S3 and WebDAV sync through the sync transport"
```

---

### Task 5: 全局代理忽略内嵌监听地址

**Files:**
- Create: `kernel/model/network_proxy.go`
- Modify: `kernel/model/conf.go:579`
- Modify: `kernel/model/conf_specs.go:149-155`
- Modify: `kernel/api/system.go`（`setTailscale` 保存后调用 `model.ApplyNetworkProxy()`）
- Test: `kernel/model/network_proxy_test.go`

**Interfaces:**
- Consumes: `tailscale.IsEmbeddedProxy(proxyURL string) bool`（Task 1）。
- Produces:
  - `func ApplyNetworkProxy()`（读 `Conf.System.Load().NetworkProxy` 后调用内部实现）
  - 内部 `func applyNetworkProxy(proxy *conf.NetworkProxy)`
  - 内部 `func effectiveNetworkProxy(proxy *conf.NetworkProxy) (value string, useSystem bool)`

- [ ] **Step 1: 写失败测试**

```go
func TestEffectiveNetworkProxyIgnoresEmbeddedListener(t *testing.T) {
	oldHome := util.HomeDir
	util.HomeDir = t.TempDir()
	t.Cleanup(func() { util.HomeDir = oldHome })
	configDir := tailscale.ConfigDir()
	if err := os.MkdirAll(configDir, 0755); nil != err {
		t.Fatalf("create config dir: %s", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tailscale.json"), []byte(`{"enabled":true,"listen":"127.0.0.1:1055"}`), 0600); nil != err {
		t.Fatalf("write config: %s", err)
	}

	value, useSystem := effectiveNetworkProxy(&conf.NetworkProxy{Scheme: "socks5", Host: "127.0.0.1", Port: "1055"})
	if "" != value || useSystem {
		t.Fatalf("embedded listener should be ignored, got %q system=%v", value, useSystem)
	}
	value, _ = effectiveNetworkProxy(&conf.NetworkProxy{Scheme: "socks5", Host: "127.0.0.1", Port: "1080"})
	if "socks5://127.0.0.1:1080" != value {
		t.Fatalf("real proxy should be preserved, got %q", value)
	}
	_, useSystem = effectiveNetworkProxy(&conf.NetworkProxy{Scheme: "system"})
	if !useSystem {
		t.Fatal("system proxy should stay system")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./model -run TestEffectiveNetworkProxyIgnoresEmbeddedListener -v`
Expected: FAIL，`undefined: effectiveNetworkProxy`。

- [ ] **Step 3: 实现**

- `network_proxy.go`：`effectiveNetworkProxy` 在 `proxy.IsSystem()` 时返回 `("", true)`；否则取 `proxy.String()`，若 `tailscale.IsEmbeddedProxy` 为 true 则返回 `("", false)`，否则原值。`applyNetworkProxy(proxy)` 调用 `util.SetNetworkProxy(value, useSystem)`，忽略时记一条 info 日志；`ApplyNetworkProxy()` 用 `Conf.System.Load().NetworkProxy` 调用它。
- `conf.go` 的启动调用改成 `applyNetworkProxy(Conf.System.Load().NetworkProxy)`；`conf_specs.go` 的 `Reaction.Apply` 改成 `applyNetworkProxy(next.NetworkProxy)`（用 `next`，不要读回 store 后的值）。
- `api/system.go` 的 `setTailscale` 在 `SaveConfig` 成功后调用 `model.ApplyNetworkProxy()`（监听地址可能变化）。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && gofmt -w model/network_proxy.go model/network_proxy_test.go model/conf.go model/conf_specs.go api/system.go && go test ./model -run TestEffectiveNetworkProxy -v`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add kernel/model/network_proxy.go kernel/model/network_proxy_test.go kernel/model/conf.go kernel/model/conf_specs.go kernel/api/system.go
git commit -m ":bug: Keep the embedded Tailscale listener out of the global proxy"
```

---

### Task 6: 登录后自动补同步与单条常驻提示

**Files:**
- Create: `kernel/model/sync_tailscale.go`
- Modify: `kernel/model/sync.go:37-49`（`publishSyncResult`）
- Modify: `kernel/model/conf.go`（`InitConf` 注册 `tailscale.SetOnRunning`）
- Test: `kernel/model/sync_tailscale_test.go`

**Interfaces:**
- Consumes: `tailscale.IsBlocked()`、`tailscale.ConsumePendingRetry()`、`tailscale.SetOnRunning`、`tailscale.SyncBlockedMessage()`。
- Produces:
  - 内部 `func onTailscaleReady()`
  - 内部 `func showTailscaleSyncNotice()` / `func hideTailscaleSyncNotice()`
  - 测试钩子 `var syncBlocked = tailscale.IsBlocked`、`var consumeTailscaleRetry = tailscale.ConsumePendingRetry`
  - `var defaultTailscaleSync = func() { go func() { _ = SyncData(false) }() }`、`var triggerTailscaleSync = defaultTailscaleSync`

- [ ] **Step 1: 写失败测试**

```go
func TestOnTailscaleReadyRetriesOnce(t *testing.T) {
	consumed := 0
	triggered := 0
	consumeTailscaleRetry = func() bool { consumed++; return 1 == consumed }
	triggerTailscaleSync = func() { triggered++ }
	t.Cleanup(func() { consumeTailscaleRetry = tailscale.ConsumePendingRetry; triggerTailscaleSync = defaultTailscaleSync })
	onTailscaleReady()
	onTailscaleReady()
	if 1 != triggered {
		t.Fatalf("sync triggers = %d, want 1", triggered)
	}
}

func TestPublishSyncResultShowsOneBlockedNotice(t *testing.T) {
	tailscaleNoticeID = ""
	syncBlocked = func() bool { return true }
	t.Cleanup(func() { syncBlocked = tailscale.IsBlocked; hideTailscaleSyncNotice() })
	publishSyncResult(2, errors.New("socks dial failed"))
	first := tailscaleNoticeID
	publishSyncResult(2, errors.New("socks dial failed"))
	if "" == first || first != tailscaleNoticeID {
		t.Fatalf("notice ID should stay stable, first=%q now=%q", first, tailscaleNoticeID)
	}
	publishSyncResult(1, nil)
	if "" != tailscaleNoticeID {
		t.Fatal("success should clear the notice")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./model -run 'TestOnTailscaleReady|TestPublishSyncResultShowsOneBlockedNotice' -v`
Expected: FAIL，`undefined: onTailscaleReady`。

> 注：`publishSyncResult` 依赖 `Conf.Sync` 已初始化；测试按 `kernel/model/sync_result_test.go` 的方式先 `Conf.Sync.Store(&conf.Sync{})`，并在结束后恢复。

- [ ] **Step 3: 实现**

- `sync_tailscale.go`：
  - `tailscaleNoticeID string` + `sync.Mutex`（或复用 `syncCloudIndexListNotice` 的 observer 结构，但保持独立提示）。
  - `showTailscaleSyncNotice`：ID 为空时 `util.PushErrMsg(tailscale.SyncBlockedMessage(), 0)` 并保存 ID。
  - `hideTailscaleSyncNotice`：ID 非空时 `util.PushClearMsg(ID)` 并清空。
  - `onTailscaleReady`：`if !consumeTailscaleRetry() { return }`；`hideTailscaleSyncNotice()`；`triggerTailscaleSync()`。
- `publishSyncResult`：
  ```go
  blocked := 2 == code && nil != syncErr && syncBlocked()
  snapshot := UpdateSyncConf(func(s *conf.Sync) {
      s.LastResult = code
      if syncErr != nil {
          if blocked { s.Stat = tailscale.SyncBlockedMessage() } else { s.Stat = formatSyncRepoErrorMsg(syncErr) }
      } else if code == 1 { s.Synced = util.CurrentTimeMillis() }
  })
  if blocked { showTailscaleSyncNotice() } else if 1 == code { hideTailscaleSyncNotice() }
  util.BroadcastByType("main", "syncing", code, snapshot.Stat, nil)
  ```
- `conf.go` 的 `InitConf` 末尾：`tailscale.SetOnRunning(onTailscaleReady)`（`SetOnRunning` 应覆盖而不是重复追加；在 Task 2 中实现为替换唯一回调，避免重复注册）。
- 注意：`SyncData(false)` 会走现有同步锁；已有同步进行时自动返回，不重复执行。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && gofmt -w model/sync_tailscale.go model/sync.go model/conf.go model/sync_tailscale_test.go && go test ./model -run 'TestOnTailscaleReady|TestPublishSyncResultShowsOneBlockedNotice' -v`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add kernel/model/sync_tailscale.go kernel/model/sync.go kernel/model/conf.go kernel/model/sync_tailscale_test.go
git commit -m ":bug: Resume sync and keep one notice when Tailscale login completes"
```

---

### Task 7: API 契约与生成类型

**Files:**
- Modify: `kernel/apicontract/basics.go`
- Modify: `kernel/api/system.go`（`tailscaleData`、`setTailscale`）
- Modify: `kernel/api/tailscale_test.go`
- Generated: `app/src/types/api/index.d.ts`（由 `pnpm run api:generate` 产生）

**Interfaces:**
- Produces:
  - `apicontract.TailscaleConfig.SyncNetwork string`（JSON `syncNetwork`）
  - `apicontract.TailscaleConfigRequest.SyncNetwork string`
  - `apicontract.TailscaleStatus.KeyExpiry string`（JSON `keyExpiry`）
  - `tailscaleData()` 映射 `config.SyncNetwork`、`status.KeyExpiry`
  - `setTailscale` 把 `request.SyncNetwork` 写入 `tailscale.Config`

- [ ] **Step 1: 写失败测试**

在 `kernel/api/tailscale_test.go` 增加：

```go
func TestTailscaleDataReportsSyncNetworkAndKeyExpiry(t *testing.T) {
	oldHomeDir := util.HomeDir
	util.HomeDir = t.TempDir()
	t.Cleanup(func() { util.HomeDir = oldHomeDir })
	configDir := tailscale.ConfigDir()
	if err := os.MkdirAll(configDir, 0755); nil != err {
		t.Fatalf("create config dir: %s", err)
	}
	configPath := filepath.Join(configDir, "tailscale.json")
	if err := os.WriteFile(configPath, []byte(`{"enabled":true,"listen":"127.0.0.1:2055","syncNetwork":"direct"}`), 0600); nil != err {
		t.Fatalf("write config: %s", err)
	}
	data := tailscaleData()
	if "direct" != data.Config.SyncNetwork {
		t.Fatalf("syncNetwork = %q, want direct", data.Config.SyncNetwork)
	}
	if "" != data.Status.KeyExpiry {
		t.Fatalf("keyExpiry = %q, want empty for a disabled node", data.Status.KeyExpiry)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd kernel && go test ./api -run TestTailscaleDataReportsSyncNetworkAndKeyExpiry -v`
Expected: FAIL，`data.Config.SyncNetwork` 不存在。

- [ ] **Step 3: 实现**

- `apicontract/basics.go`：两个结构体加字段与注释；`TailscaleStatus` 注释更新为包含 `needsMachineAuth` 与 `keyExpiry`。
- `api/system.go`：`setTailscale` 的 `tailscale.Config` 加 `SyncNetwork: request.SyncNetwork`；`tailscaleData` 的 Config/Status 映射新字段。
- `cd app && pnpm run api:generate`，随后 `pnpm run api:check`。
- 检查生成的 `app/src/types/api/index.d.ts` 只包含预期的字段变化。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd kernel && gofmt -w apicontract/basics.go api/system.go api/tailscale_test.go && go test ./api -run TestTailscale -v && go test ./apicontract/...`
Run: `cd app && pnpm run api:check`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add kernel/apicontract/basics.go kernel/api/system.go kernel/api/tailscale_test.go app/src/types/api/index.d.ts
git commit -m ":recycle: Expose the Tailscale sync network mode and key expiry"
```

---

### Task 8: 设置页「同步网络」开关

**Files:**
- Create: `app/src/config/tabs/tailscaleSyncNetwork.ts`
- Create: `app/src/config/tabs/tailscaleSyncNetwork.test.ts`
- Modify: `app/src/config/tabs/appTab.ts`（`genTailscaleHtml`、`mountTailscale`）
- Modify: `app/src/types/index.d.ts`（语言键）
- Modify: `app/appearance/langs/*.json`（4 个新键，23 个文件）

**Interfaces:**
- Produces:
  - `type TailscaleSyncNetwork = "direct" | "proxy"`
  - `func normalizeSyncNetwork(value: unknown): TailscaleSyncNetwork`
- UI：`#tailscaleSyncNetwork` 下拉，值为 `direct`/`proxy`；保存时随 `setTailscale` 提交。

- [ ] **Step 1: 写失败测试**

`tailscaleSyncNetwork.test.ts`（沿用 `tailscaleStatus.test.ts` 的 `transpileModule` + `runInNewContext`）：

```ts
test("normalizeSyncNetwork 只接受 proxy，其余回退 direct", () => {
    const {normalizeSyncNetwork} = load();
    assert.equal(normalizeSyncNetwork("proxy"), "proxy");
    assert.equal(normalizeSyncNetwork("direct"), "direct");
    assert.equal(normalizeSyncNetwork(""), "direct");
    assert.equal(normalizeSyncNetwork(undefined), "direct");
    assert.equal(normalizeSyncNetwork("bogus"), "direct");
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd app && pnpm run test:focused src/config/tabs/tailscaleSyncNetwork.test.ts`
Expected: FAIL，模块不存在。

- [ ] **Step 3: 实现**

- `tailscaleSyncNetwork.ts`：导出类型与 `normalizeSyncNetwork`。
- `appTab.ts`：
  - `genTailscaleHtml` 在监听输入行后增加：
    ```html
    <div class="b3-label__text fn__flex" style="overflow: visible !important;">
        <select id="tailscaleSyncNetwork" class="b3-select fn__flex-1">
            <option value="direct">${window.siyuan.languages.tailscaleSyncNetworkDirect}</option>
            <option value="proxy">${window.siyuan.languages.tailscaleSyncNetworkProxy}</option>
        </select>
    </div>
    <div class="b3-label__text">${window.siyuan.languages.tailscaleSyncNetworkTip}</div>
    ```
  - `mountTailscale` 取 `#tailscaleSyncNetwork`；`applyData` 设 `normalizeSyncNetwork(data.config.syncNetwork)`；`save` 带 `syncNetwork`。
  - `#tailscaleSyncNetwork` 缺失时保持现有 return（与其它元素一致）。
- `app/src/types/index.d.ts` 的 `ILanguages` 增加 4 个可选键。
- 23 个 `app/appearance/langs/*.json` 顶部加 4 个键并翻译；`python scripts/check-lang-keys.py`。

- [ ] **Step 4: 跑检查确认通过**

Run: `cd app && pnpm run test:focused src/config/tabs/tailscaleSyncNetwork.test.ts && pnpm run typecheck`
Run: `python scripts/check-lang-keys.py`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add app/src/config/tabs/tailscaleSyncNetwork.ts app/src/config/tabs/tailscaleSyncNetwork.test.ts app/src/config/tabs/appTab.ts app/src/types/index.d.ts app/appearance/langs
git commit -m ":lipstick: Add the Tailscale sync network switch"
```

---

### Task 9: 登录状态视图（待批准、密钥过期、轮询）

**Files:**
- Modify: `app/src/config/tabs/tailscaleStatus.ts`
- Modify: `app/src/config/tabs/tailscaleStatus.test.ts`
- Modify: `app/src/config/tabs/appTab.ts`（`renderStatus` 传 `keyExpiry`；轮询间隔）
- Modify: `app/src/types/index.d.ts`（4 个语言键）
- Modify: `app/appearance/langs/*.json`（4 个新键：3 个状态文案 + `tailscaleSyncBlocked`）

**Interfaces:**
- Consumes: `TailscaleStatus.keyExpiry`（Task 7）。
- Produces:
  - `ITailscaleStatus` 增加 `keyExpiry?: string`
  - `ITailscaleLanguages` 增加 `tailscaleNeedsMachineAuth`、`tailscaleKeyExpired`、`tailscaleKeyExpiring`
  - `describeTailscaleStatus(status, languages, now = Date.now())` 支持新状态与过期提醒
  - i18n `tailscaleSyncBlocked` 供内核常驻提示使用（Task 6）
- UI：`needsLogin` / `starting` / `needsMachineAuth` 时 2 秒轮询，其余 10 秒。

- [ ] **Step 1: 写失败测试**

在 `tailscaleStatus.test.ts` 增加：

```ts
test("待管理员批准时不显示登录链接", () => {
    const ret = load().describeTailscaleStatus(status("needsMachineAuth", "https://login.tailscale.com/a/9"), languages);
    assert.equal(ret.text, "等待管理员批准设备");
    assert.equal(ret.loginURL, "");
});

test("登录过期使用过期文案", () => {
    const now = Date.parse("2026-11-27T00:00:00Z");
    const ret = load().describeTailscaleStatus(
        {state: "needsLogin", authURL: "", ips: null, error: "", keyExpiry: "2026-11-01T00:00:00Z"}, languages, now);
    assert.equal(ret.text, "登录已过期，请重新登录");
});

test("运行中且密钥 7 天内过期时追加提醒", () => {
    const now = Date.parse("2026-11-27T00:00:00Z");
    const ret = load().describeTailscaleStatus(
        {state: "running", authURL: "", ips: null, error: "", keyExpiry: "2026-12-01T00:00:00Z"}, languages, now);
    assert.equal(ret.text, "已连接 · 设备密钥将于 2026-12-01 过期");
});
```

> `languages` 对象补上 `tailscaleNeedsMachineAuth: "等待管理员批准设备"`、`tailscaleKeyExpired: "登录已过期，请重新登录"`、`tailscaleKeyExpiring: "设备密钥将于 ${date} 过期"`；`describeTailscaleStatus` 用 `toISOString().slice(0, 10)` 填 `${date}`，测试可稳定断言。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd app && pnpm run test:focused src/config/tabs/tailscaleStatus.test.ts`
Expected: FAIL，`needsMachineAuth` 文案为空 / `keyExpiry` 未处理。

- [ ] **Step 3: 实现**

- `tailscaleStatus.ts`：
  - `ITailscaleStatus` 加 `keyExpiry?: string`；`ITailscaleLanguages` 加 3 个键；`app/src/types/index.d.ts` 的 `ILanguages` 加 4 个可选键（含 `tailscaleSyncBlocked`）。
  - `needsMachineAuth` → `text = languages.tailscaleNeedsMachineAuth`，`loginURL = ""`。
  - `needsLogin` 且 `keyExpiry` 已过期 → `text = languages.tailscaleKeyExpired`。
  - `running` 且 `keyExpiry` 在 7 天内 → 文案追加 ` · ` + `tailscaleKeyExpiring.replace("${date}", yyyy-mm-dd)`。
  - 保留现有规则：`running` 不返回链接；其余状态 `authURL` 非空即返回。
- `appTab.ts`：`renderStatus` 传 `keyExpiry`；轮询 `setTimeout(poll, (state === "needsLogin" || state === "starting" || state === "needsMachineAuth") ? 2000 : 10000)`（运行状态 10 秒）。
- 23 个语言文件加 4 个键（`tailscaleNeedsMachineAuth`、`tailscaleKeyExpired`、`tailscaleKeyExpiring`、`tailscaleSyncBlocked`）并翻译；`python scripts/check-lang-keys.py`。

- [ ] **Step 4: 跑检查确认通过**

Run: `cd app && pnpm run test:focused src/config/tabs/tailscaleStatus.test.ts && pnpm run typecheck`
Run: `python scripts/check-lang-keys.py`
Expected: PASS。

- [ ] **Step 5: 提交（需用户明确同意后执行）**

```bash
git add app/src/config/tabs/tailscaleStatus.ts app/src/config/tabs/tailscaleStatus.test.ts app/src/config/tabs/appTab.ts app/src/types/index.d.ts app/appearance/langs
git commit -m ":lipstick: Show Tailscale machine approval and key expiry"
```

---

### Task 10: 全量验证与文档

**Files:**
- 无新增产品文件；只跑检查、修正测试，必要时补 `docs/`。

- [ ] **Step 1: Go 格式与测试**

Run:
```bash
cd kernel
gofmt -w tailscale/*.go model/network_proxy.go model/sync_tailscale.go model/repository.go model/sync_probe.go model/conf.go model/conf_specs.go api/system.go api/tailscale_test.go apicontract/basics.go
go test ./tailscale/... ./apicontract/...
go test ./model -run 'TestS3Probe|TestEffectiveNetworkProxy|TestOnTailscaleReady|TestPublishSyncResult' -v
go test ./api -run TestTailscale -v
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./tailscale/
```
Expected: PASS；移动端交叉编译若因本机工具链限制失败，记录具体错误并说明未验证项，不要伪称通过。

- [ ] **Step 2: 前端 lint / typecheck / 契约**

Run:
```bash
cd app
pnpm run test:focused src/config/tabs/tailscaleStatus.test.ts src/config/tabs/tailscaleSyncNetwork.test.ts
pnpm run test:changed --explain
pnpm run api:check
pnpm run lint
```
Expected: PASS；`pnpm run lint` 只产生本任务范围内的自动修复，检查 `git diff` 没有无关改动。

- [ ] **Step 3: i18n 与指南**

Run:
```bash
python scripts/check-lang-keys.py
rg -l "Tailscale|tailscale" app/guide || true
```
Expected: 语言键完整；`app/guide` 没有 Tailscale 文档，因此无需改指南（在回复中说明）。

- [ ] **Step 4: 汇总验证结果**

在最终回复中列出：跑过的命令、通过/失败、未跑项及原因；不要声称未验证的行为。

- [ ] **Step 5: 提交（需用户明确同意后执行，可合并为一个提交）**

```bash
git add docs/superpowers/specs/2026-11-27-tailscale-sync-network-design.md docs/superpowers/plans/2026-11-27-tailscale-sync-network.md
git commit -m ":memo: Document the Tailscale sync network design and plan"
```