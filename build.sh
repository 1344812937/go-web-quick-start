#!/bin/bash

set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$ROOT_DIR"

# project.json 维护稳定项目身份；PACKAGE_NAME 只控制本次构建产物名。
PROJECT_BINARY=$(go run ./tools/projectctl field binaryName)
VERSION=$(go run ./tools/projectctl field version)

branch_package_name() {
	local branch_name="$1"
	local semantic_name="$branch_name"

	semantic_name="${semantic_name#refs/heads/}"
	semantic_name="${semantic_name#codex/}"
	case "$semantic_name" in
		feature/*|feat/*|fix/*|bugfix/*|hotfix/*|chore/*|release/*)
			semantic_name="${semantic_name#*/}"
			;;
	esac

	printf '%s' "$semantic_name" \
		| tr '[:upper:]' '[:lower:]' \
		| sed -E 's/[^a-z0-9._-]+/-/g; s/^[._-]+//; s/[._-]+$//'
}

REQUESTED_PACKAGE_NAME="${PACKAGE_NAME:-}"
CURRENT_BRANCH=$(git branch --show-current 2>/dev/null || true)

if [ -n "$REQUESTED_PACKAGE_NAME" ]; then
	if [[ ! "$REQUESTED_PACKAGE_NAME" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
		echo "PACKAGE_NAME 只能包含字母、数字、点、下划线和短横线，并且必须以字母或数字开头"
		exit 1
	fi
	APP_NAME="$REQUESTED_PACKAGE_NAME"
	NAME_SOURCE="PACKAGE_NAME"
elif [ -n "$CURRENT_BRANCH" ] && [ "$CURRENT_BRANCH" != "main" ] && [ "$CURRENT_BRANCH" != "master" ]; then
	APP_NAME=$(branch_package_name "$CURRENT_BRANCH")
	if [ -z "$APP_NAME" ]; then
		APP_NAME="$PROJECT_BINARY"
		NAME_SOURCE="project.json（分支名无法生成可移植文件名）"
	else
		NAME_SOURCE="分支 $CURRENT_BRANCH"
	fi
else
	APP_NAME="$PROJECT_BINARY"
	NAME_SOURCE="project.json（main/master 或 detached HEAD）"
fi

if [ -z "$VERSION" ]; then
	echo "无法从 project.json 读取版本号"
	exit 1
fi

echo "构建 ${APP_NAME} ${VERSION}"

rm -rf build
mkdir -p build

frontend_started_at=$SECONDS
frontend_log=$(mktemp "${TMPDIR:-/tmp}/go-web-quick-start-frontend.XXXXXX")
trap 'rm -f "$frontend_log"' EXIT
if ! pnpm --dir frontend build >"$frontend_log" 2>&1; then
	echo "前端构建失败："
	tail -n 40 "$frontend_log"
	exit 1
fi
printf '前端构建耗时：%ss\n' "$((SECONDS - frontend_started_at))"

go_started_at=$SECONDS

build_target() {
	local goos="$1"
	local goarch="$2"
	local output="build/${APP_NAME}-${goos}-${goarch}-${VERSION}"
	if [ "$goos" = "windows" ]; then
		output+=".exe"
	fi
	echo "+ ${goos}/${goarch} -> ${output}"
	GOOS="$goos" GOARCH="$goarch" go build -buildvcs=false -o "$output" .
}

for target in \
	"linux amd64" "linux arm64" \
	"windows amd64" "windows arm64" \
	"darwin amd64" "darwin arm64"; do
	read -r goos goarch <<<"$target"
	build_target "$goos" "$goarch"
done
printf 'Go 交叉编译耗时：%ss\n' "$((SECONDS - go_started_at))"
printf '构建完成，共生成 6 个产物，总耗时：%ss\n' "$SECONDS"
ls -lh build/
