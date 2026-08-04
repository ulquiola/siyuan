# 构建脚本自动设置 TARGETPLATFORM，无需手动传参
# go-build 阶段使用 BUILDPLATFORM 原生编译，然后交叉编译到 TARGETARCH
# 这样完全避开 QEMU 模拟，解决 Go 1.26 编译器段错误问题

FROM --platform=$BUILDPLATFORM node:22 AS node-build

ARG NPM_REGISTRY=

WORKDIR /app
ADD app/package.json app/pnpm* app/.npmrc .

RUN <<EORUN
#!/bin/bash -e
corepack enable
corepack install --global $(node -e 'console.log(require("./package.json").packageManager)')
npm config set registry ${NPM_REGISTRY}
pnpm install --silent
EORUN

ADD app/ .
RUN <<EORUN
#!/bin/bash -e
pnpm run build
node scripts/trimChangelogs.js
mkdir /artifacts
mv appearance stage guide /artifacts/
if [ -d changelogs ]; then mv changelogs /artifacts/; fi
EORUN

FROM --platform=$BUILDPLATFORM golang:1.26 AS go-build

# 目标架构（由 buildx 自动设置：amd64 或 arm64）
ARG TARGETARCH=amd64
ARG BUILDPLATFORM

# 替换 Debian 源为阿里云镜像
RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || \
    sed -i 's|httpredir.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list 2>/dev/null || true

# 安装 musl 交叉编译工具链（用于交叉编译 CGO 到目标架构）
# musl 生成静态链接二进制，可在 Alpine 运行
RUN <<EORUN
#!/bin/sh -e
apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    wget \
    xz-utils
# 下载并安装 musl 交叉编译器
MUSL_ARCH=""
case "$TARGETARCH" in
  amd64) MUSL_ARCH="x86_64-linux-musl" ;;
  arm64) MUSL_ARCH="aarch64-linux-musl" ;;
esac
wget -qO /tmp/musl.tar.xz "https://musl.cc/${MUSL_ARCH}-cross.tgz"
tar -xf /tmp/musl.tar.xz -C /opt
ln -s /opt/${MUSL_ARCH}-cross/bin/${MUSL_ARCH}-gcc /usr/local/bin/${MUSL_ARCH}-gcc
go env -w GO111MODULE=on
go env -w CGO_ENABLED=1
go env -w GOOS=linux
go env -w GOARCH=$TARGETARCH
go env -w CC=${MUSL_ARCH}-gcc
EORUN

WORKDIR /kernel
ADD kernel/go.* .
# 原生编译（无 QEMU），交叉编译到目标架构
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg \
    GOPROXY=https://mirrors.aliyun.com/goproxy/,https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn GOGC=off GOFLAGS=-mod=mod go mod download

ADD kernel/ .
# 原生编译 + musl 交叉编译，完全避开 QEMU 段错误
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg \
    GOGC=off GOFLAGS=-mod=mod go build -p 1 -tags fts5 -ldflags "-s -w"

FROM alpine:latest
LABEL maintainer="Liang Ding<845765@qq.com>"
LABEL modifier="ulquiola@163.com>"

RUN sed -i "s|https://dl-cdn.alpinelinux.org|https://mirrors.aliyun.com|g" /etc/apk/repositories && apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai
ENV RUN_IN_CONTAINER=true
ENV LANG=zh_CN.UTF-8
ENV LC_ALL=zh_CN.UTF-8
EXPOSE 6806
VOLUME /siyuan/workspace

WORKDIR /opt/siyuan/
COPY --from=go-build --chmod=755 /kernel/kernel /kernel/entrypoint.sh .
COPY --from=node-build /artifacts .

ENTRYPOINT ["/opt/siyuan/entrypoint.sh"]
# 默认启动伺服。若通过 `docker run` / `command:` 传额外参数，需自行带上 `serve` 子命令，
# 否则用户参数会整体覆盖 CMD。
CMD ["/opt/siyuan/kernel", "serve"]