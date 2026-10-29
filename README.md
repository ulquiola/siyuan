# SiYuan

本仓库为 SiYuan 项目（内核 + 前端）的源码与部署辅助文件集合，基于上游 v3.8.8，并包含少量自用调整：

- 移除设置中的账户与订阅入口；
- 第三方同步的付费校验直接放行；
- 桌面版内核内置 Tailscale（tsnet），可在 设置 - 应用 - Tailscale 中启用并查看状态，无需管理员权限即可通过本机 SOCKS5 代理访问 Tailnet；
- Docker 构建适配国内网络与 amd64 原生编译。

版本号跟随上游，当前为 `3.8.8`；推送 `v*` 标签会触发 `Release` 工作流，构建 Windows 与 macOS 安装包并创建 GitHub Release。

目录说明：

- `kernel/`：Go 服务端，负责数据存储、同步、API、OCR 等；
- `app/`：Electron/TypeScript 前端，包含编辑器与导出渲染器；
- `scripts/`：构建、打包、发布与检查脚本。

## 快速开始

### 容器运行

将下面的内容保存为 `docker-compose.yml`，修改工作区路径和访问密码后执行 `docker compose up -d`：

```yaml
services:
  siyuan:
    image: registry.cn-hangzhou.aliyuncs.com/ulquiola/siyuan:latest
    # image: docker.1ms.run/ulquiola/siyuan:latest
    container_name: siyuan
    restart: always
    environment:
      - LANG=zh_CN.UTF-8
      - LC_ALL=zh_CN.UTF-8
    ports:
      - "6806:6806"
    labels:
      watchcow.enable: "true"
    volumes:
      - /home/ulquiola/codespace/siyuan/workspace:/siyuan/workspace
    command: serve --workspace=/siyuan/workspace --accessAuthCode=123456
```

示例命令：

```shell
docker compose up -d
docker compose logs -f siyuan
```

访问 `http://<主机地址>:6806`。如果系统配置了 HTTP 代理，请把主机地址加入代理绕过列表，否则浏览器的 WebSocket 可能被代理拦截，页面会提示与内核的连接已断开。

### 桌面版内置 Tailscale

内核中嵌入了 Tailscale 官方的 `tsnet` 库，用用户态网络栈加入 Tailnet，不需要 WSL2、Docker 或管理员权限。启用后内核在本机监听一个 SOCKS5 端口，思源设置中的网络代理指向该端口即可访问 Tailnet 内的地址（例如 NAS 上的 S3 服务）。

打开 设置 - 应用 - Tailscale：

- 打开"启用"开关；
- 节点名留空时使用 `siyuan-desktop`；
- 认证密钥填 Tailscale 管理台 Settings - Keys 中生成的 reusable key，首次加入成功后可以清空，节点状态会保存在本地；
- 监听地址留空时使用 `127.0.0.1:1055`，只允许回环地址；
- 状态行会显示未启用、正在连接、需要登录、已连接（含 Tailnet IP）或错误原因，需要登录时点击"打开登录链接"用浏览器完成认证。

保存后内核会立即重建内嵌节点，不需要重启思源。

随后在 设置 - 网络代理 中选择 `socks5`、主机 `127.0.0.1`、端口 `1055`；S3 的 Endpoint 填 NAS 的 Tailnet 地址（`100.x.y.z`）。

配置保存在 `~/.config/siyuan/tailscale.json`（Windows 为 `C:\Users\<用户名>\.config\siyuan\tailscale.json`），由界面写入，文件不存在时该功能完全关闭。可选字段：`stateDir` 指定节点状态目录，`enabled` 设为 `false` 临时关闭。

注意事项：

- 启动日志位于 `~/.config/siyuan/kernel.log`，搜索 `tailscale:` 可以确认节点是否上线；
- 如果 1055 端口已被 WSL2 中的 Docker 容器占用，需要先停止该容器，否则内嵌节点无法监听；
- 该功能需要自行构建内核。在 GitHub Actions 中手动运行 `Windows kernel` 工作流，下载构建产物后替换安装目录下的 `resources\kernel\SiYuan-Kernel.exe`。

### 镜像构建与发布

构建脚本会读取仓库根目录的 `.env`（需要 `ALIYUN_USERNAME`、`ALIYUN_PASSWORD`），登录阿里云镜像仓库，构建 `linux/amd64` 镜像并推送 `v<版本>` 与 `latest` 标签：

```shell
sh scripts/docker-build.sh
```

Dockerfile 包含前端构建、Go 内核（`fts5` + `sqlcipher`）以及 ONNX/OCR 运行时。Go 阶段在 `BUILDPLATFORM` 上原生编译并交叉编译到目标架构，避免 QEMU 模拟下的编译器问题。

### 本地开发

需要 Go（版本见 `kernel/go.mod`）和 Node + pnpm（版本见 `app/package.json` 的 `packageManager`）。

前端检查（在 `app/` 目录下）：

```shell
pnpm install
pnpm run lint
```

`pnpm run lint` 包含类型检查、ESLint 自动修复、CSS、菜单和启动语言检查。

内核开发（在 `kernel/` 目录下）：

```shell
gofmt -w .
go build ./...
```

更多构建、打包、i18n 与发布细节请参考 `AGENTS.md` 和 `scripts/` 目录。

## 版本与发布

版本号跟随上游，当前为 `3.8.8`。发布时在主仓库根目录创建并推送标签：

```shell
git tag v3.8.8
git push github v3.8.8
git push origin v3.8.8
```

推送标签后，`Release` 工作流会并行构建 Windows x64、macOS Intel 和 macOS Apple Silicon 三个安装包，并创建 GitHub Release。资产为：

- `siyuan-<版本>-win.exe`：Windows x64 安装包
- `siyuan-<版本>-mac.dmg`：macOS Intel 安装包
- `siyuan-<版本>-mac-arm64.dmg`：macOS Apple Silicon 安装包
- `SHA256SUMS.txt`：SHA-256 校验值

安装包内已包含内置 Tailscale 的内核。macOS 安装包没有签名和公证，首次打开需要在 系统设置 - 隐私与安全性 中允许，或执行 `xattr -cr /Applications/SiYuan.app`。

Docker 镜像不通过该工作流发布，使用 `scripts/docker-build.sh` 推送到阿里云仓库，标签为 `v<版本>` 与 `latest`。

上游官方的正式版发布流程（`dev` 合并到 `master`、签名、公证以及 Android 与鸿蒙产物）依赖官方构建环境，不适用于本仓库。

## 贡献

欢迎提交 Issue 或 PR，请先阅读 `.github/CONTRIBUTING.md`，并遵循 `AGENTS.md` 中的代码规范。

## 许可

本项目遵循 AGPL-3.0 许可，详见 `LICENSE`。
