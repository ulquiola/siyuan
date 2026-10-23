# SiYuan

本仓库为 SiYuan 项目（内核 + 前端）的源码与部署辅助文件集合，基于上游 v3.8.8，并包含少量自用调整：

- 移除设置中的账户与订阅入口；
- 第三方同步的付费校验直接放行；
- Docker 构建适配国内网络与 amd64 原生编译。

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

## 贡献

欢迎提交 Issue 或 PR，请先阅读 `.github/CONTRIBUTING.md`，并遵循 `AGENTS.md` 中的代码规范。

## 许可

本项目遵循 AGPL-3.0 许可，详见 `LICENSE`。
