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

WORKDIR /opt/siyuan/
COPY --from=GO_BUILD /opt/siyuan/ /opt/siyuan/

RUN apk add --no-cache ca-certificates tzdata && \
    chmod +x /opt/siyuan/entrypoint.sh

ENV TZ=Asia/Shanghai
ENV RUN_IN_CONTAINER=true
ENV LANG=zh_CN.UTF-8
ENV LC_ALL=zh_CN.UTF-8
EXPOSE 6806
VOLUME /siyuan/workspace

ENTRYPOINT ["/opt/siyuan/entrypoint.sh"]
CMD ["--workspace=/siyuan/workspace", "--accessAuthCode=password"]
