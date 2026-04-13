#!/bin/bash
# build.sh

set -euo pipefail

# 设置变量
APP_NAME="tank-tool"
VERSION=$(sed -n 's/^const AppVersion = "\([^"]*\)"$/\1/p' internal/app/meta.go)

if [ -z "$VERSION" ]; then
	echo "无法从 internal/app/meta.go 读取版本号"
	exit 1
fi

# 清理之前的构建
rm -rf build
mkdir -p build

build_target() {
	local goos="$1"
	local goarch="$2"
	local output="$3"
	GOOS="$goos" GOARCH="$goarch" go build -buildvcs=false -o "$output" .
}

echo "+ Linux！"
# Linux 版本
build_target linux amd64 "build/${APP_NAME}-linux-amd64-${VERSION}"

echo "+ Windows！"
# Windows 版本
build_target windows amd64 "build/${APP_NAME}-windows-amd64-${VERSION}.exe"

echo "+ macOS！"
# macOS 版本（可选）
build_target darwin amd64 "build/${APP_NAME}-darwin-amd64-${VERSION}"

echo "✅ 构建完成！"
ls -lh build/