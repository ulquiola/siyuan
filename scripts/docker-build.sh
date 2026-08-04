#!/usr/bin/env sh
set -eu

# 加载环境变量
if [ ! -f .env ]; then
  echo "❌ 未找到 .env 文件"
  exit 1
fi
. ./.env

# 镜像仓库配置
ALIYUN_REPO="registry.cn-hangzhou.aliyuncs.com/ulquiola/siyuan"

# 获取版本号
VERSION=$(jq -r .version app/package.json 2>/dev/null || echo "unknown")
echo "✅ 当前版本：v${VERSION}"

# 同步子模块
git submodule update --init --recursive

# 登录阿里云镜像仓库
echo "🔐 登录阿里云"
echo "${ALIYUN_PASSWORD}" | docker login registry.cn-hangzhou.aliyuncs.com -u "${ALIYUN_USERNAME}" --password-stdin

# 自动生成 .dockerignore
cat > .dockerignore <<'EOF'
app/node_modules
app/.pnpm-store
.git
.DS_Store
EOF

echo "🚀 开始构建（使用国内镜像源）"
docker build \
  --platform linux/amd64 \
  --build-arg NPM_REGISTRY=https://registry.npmmirror.com \
  -t "${ALIYUN_REPO}:v${VERSION}" \
  -t "${ALIYUN_REPO}:latest" \
  .

echo "🚀 推送镜像到阿里云..."
docker push "${ALIYUN_REPO}:v${VERSION}"
docker push "${ALIYUN_REPO}:latest"

echo -e "\n🎉 构建与发布完成！"