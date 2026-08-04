# SiYuan

本仓库为 SiYuan 项目（内核 + 前端）的源码与部署辅助文件集合，包含 `kernel/`（Go 服务端）、`app/`（Electron/TypeScript 前端）等子模块。

亮点：
- 内核（`kernel/`）负责数据存储、同步、API；
- 前端（`app/`）为 Electron/web 客户端，包含导出（export）相关的渲染器；
- 支持容器化运行、插件与导出为静态 HTML/PDF 的能力。

快速开始

- 运行容器（推荐用于快速体验）

将下面的内容保存为 `docker-compose.yml`，然后执行 `docker compose up -d` 即可启动容器：

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
    command: --workspace=/siyuan/workspace --accessAuthCode=123456
```

示例命令：

```shell
docker compose up -d
docker compose logs -f siyuan
```

- 本地开发（开发者）

先安装依赖：需要安装 Go（版本见 `kernel/go.mod`）、Node + pnpm。

前端检查与开发（在 `app/` 目录下）：

```shell
cd app
pnpm install
pnpm run lint
# 其它开发脚本请参考 app/package.json
```

内核（Go）开发：

```shell
cd kernel
go build ./...
# 或使用 IDE 运行 main.go
```

更多关于构建、打包和 i18n、发布的细节请参考仓库根部的 `AGENTS.md` 与 `scripts/` 目录。

贡献与沟通

- 欢迎提交 Issue 或 PR，请先阅读仓库的 `.github/CONTRIBUTING.md`。
- 提交代码前请遵循本仓库的代码规范（见 `AGENTS.md` 中的约定）。

许可

本项目遵循 AGPL-3.0 许可（详见仓库根部的 LICENSE）。

如果需要我把文档改成英文版或者补充更多使用示例、截图或 CI 构建步骤，告诉我想要的方向。 
