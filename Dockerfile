FROM --platform=$BUILDPLATFORM node:21 AS node-build

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
mkdir /artifacts
mv appearance stage guide changelogs /artifacts/
EORUN

FROM golang:1.24-alpine AS go-build

RUN <<EORUN
#!/bin/sh -e
apk add --no-cache gcc musl-dev
go env -w GO111MODULE=on
go env -w CGO_ENABLED=1
EORUN

WORKDIR /kernel
ADD kernel/go.* .
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg \
    go mod download

ADD kernel/ .
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg \
    go build --tags fts5 -v -ldflags "-s -w"

FROM alpine:latest
LABEL maintainer="Liang Ding<845765@qq.com>"
LABEL modifier="ulquiola<ulquiola@163.com>"

RUN apk add --no-cache ca-certificates tzdata su-exec

ENV TZ=Asia/Shanghai
ENV HOME=/home/siyuan
ENV RUN_IN_CONTAINER=true
ENV LANG=zh_CN.UTF-8
ENV LC_ALL=zh_CN.UTF-8
EXPOSE 6806

WORKDIR /opt/siyuan/
# 从 Go 构建阶段复制二进制文件和入口脚本，并直接设置权限
COPY --from=go-build --chmod=755 /kernel/kernel /kernel/entrypoint.sh .
# 从 Node 构建阶段复制 UI 资源
COPY --from=node-build /artifacts .

# 显式声明一个用于持久化数据的卷
VOLUME /siyuan/workspace

# 设置容器的入口点为启动脚本
ENTRYPOINT ["/opt/siyuan/entrypoint.sh"]
# 设置默认传递给入口点的参数
# 这些参数可以被 docker run 命令中的参数覆盖
CMD ["--workspace=/siyuan/workspace", "--accessAuthCode=password"]
