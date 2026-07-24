# Go + Vue 3 Monorepo Scaffold

这是一个用于快速搭建功能的 Go + Vue 3 单仓库项目。后端使用 Gin、Wire、GORM 与 SQLite，前端使用 Vue 3、Vue Router、Vite 和 Element Plus。Go 二进制会直接嵌入前端构建产物。

项目名称、模块路径、版本、描述、令牌前缀和 favicon 统一维护在 `project.json`，不得在业务源码中重复硬编码。

## 项目结构

- `internal/`：应用、API、配置及基础设施实现
- `pkg/`：可复用的响应、模型、Repository、Service 和事务能力
- `cmd/`：Wire 依赖注入定义与生成代码
- `frontend/src/`：Vue 页面、组件、路由和前端工具
- `frontend/public/`：不经过打包转换的公共资源
- `tools/projectctl/`：项目元数据生成、校验及重命名工具

## 快速开始

环境要求：Go 1.25.4、Node.js `^20.19.0 || >=22.12.0`、pnpm 10.23.0。首次准备环境、安装失败和多平台安装方法见 [AGENTS.md](AGENTS.md) 的 Quick Start。

```bash
go mod download
cd frontend
pnpm install --frozen-lockfile
pnpm build
cd ..
go run .
```

默认入口：`http://localhost:8888/static/`。

前后端联调时分别启动：

```bash
# 终端一；首次运行前仍需保证 frontend/dist 已生成
go run .

# 终端二
pnpm --dir frontend dev
```

Vite 会把 `/api` 代理到 `http://localhost:8888`。

## 构建与校验

```bash
pnpm --dir frontend build
go run ./tools/projectctl check
go test ./...
go vet ./...
go build -buildvcs=false ./...
```

跨平台产物输出到 `build/`：

```bash
pnpm --dir frontend build
./build.sh
```

修改 `cmd/wire.go` 后重新生成依赖注入代码：

```bash
go generate ./cmd
```

## 页面与 API

| 功能 | 页面或接口 |
| --- | --- |
| 主页 | `/static/` |
| 设置页 | `/static/settings` |
| 站点信息 | `GET /api/site/info` |
| 读取设置 | `GET /api/settings` |
| 保存设置 | `PUT /api/settings` |

首次启动会在 `config/config.toml` 生成本地配置。该目录已被 Git 忽略，不要提交其中的令牌。

## 个性化项目

修改少量展示信息后执行：

```bash
go run ./tools/projectctl generate
go run ./tools/projectctl check
```

需要连 Go module 一起更改时，使用批量命令：

```bash
go run ./tools/projectctl rename \
  --module <module-path> \
  --binary <binary-name> \
  --app <application-name> \
  --display "<display name>" \
  --description "<application description>" \
  --token-prefix <token_prefix> \
  --favicon /favicon.svg
```

该命令会修改 `go.mod`、内部 Go import 和生成元数据。现有 `config/config.toml` 及已有令牌不会被迁移。

## 开发约定

当前页面只用于展示脚手架能力，不规定后续业务的 UI 风格。新增模块可重新设计主题和布局，但应保留响应式、可访问性及完整状态反馈。完整的 Git、编码、测试与安全规范见 [AGENTS.md](AGENTS.md)。
