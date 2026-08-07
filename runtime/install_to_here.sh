#!/bin/bash

set -euo pipefail

RUNTIME_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd -- "$RUNTIME_DIR/.." && pwd)

case "$(uname -s)" in
	Darwin) goos=darwin ;;
	Linux) goos=linux ;;
	MINGW*|MSYS*|CYGWIN*) goos=windows ;;
	*) echo "不支持的操作系统：$(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
	arm64|aarch64) goarch=arm64 ;;
	x86_64|amd64) goarch=amd64 ;;
	*) echo "不支持的 CPU 架构：$(uname -m)" >&2; exit 1 ;;
esac

"$ROOT_DIR/build.sh"

project_binary=$(go -C "$ROOT_DIR" run ./tools/projectctl field binaryName)
version=$(go -C "$ROOT_DIR" run ./tools/projectctl field version)
requested_package_name=${PACKAGE_NAME:-}

branch_package_name() {
	local branch_name="$1"
	local semantic_name="${branch_name#refs/heads/}"
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

if [ -n "$requested_package_name" ]; then
	app_name=$requested_package_name
else
	current_branch=$(git -C "$ROOT_DIR" branch --show-current 2>/dev/null || true)
	app_name=$project_binary
	if [ -n "$current_branch" ] && [ "$current_branch" != "main" ] && [ "$current_branch" != "master" ]; then
		app_name=$(branch_package_name "$current_branch")
		[ -n "$app_name" ] || app_name=$project_binary
	fi
fi

source_path="$ROOT_DIR/build/${app_name}-${goos}-${goarch}-${version}"
[ "$goos" = "windows" ] && source_path+=".exe"
if [ ! -f "$source_path" ]; then
	echo "未找到当前平台产物：$source_path" >&2
	exit 1
fi

target_path="$RUNTIME_DIR/$(basename "$source_path")"
mv -f "$source_path" "$target_path"
chmod +x "$target_path"
echo "已更新运行产物：$(basename "$target_path")"
