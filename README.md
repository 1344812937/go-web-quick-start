# 管区辅助工具

管区辅助工具是一个收敛到最小基础能力的 Go + Vue 3 单仓库脚手架。

当前默认命名约定如下：

- 程序展示名称：管区辅助工具
- 应用英文名：tank-helper
- 项目英文简称：tank-tool

当前仓库只保留两类程序级能力：

- 主页：展示程序基础信息、运行状态和扩展提示
- 设置：读取并保存 config/config.toml 的核心配置项

其余旧业务页面与对应工作台能力均已从运行链路中移除，适合作为新项目起点继续扩展。

## 保留内容

- Go 后端静态资源托管与 /api 基础接口容器
- Vue 3 + Vue Router + Element Plus 前端基础壳
- config/config.toml 的默认生成、启动补全与保存能力
- 统一 API 响应结构 common.R
- 前端主页与设置页

## 页面入口

| 页面 | 路径 | 说明 |
| --- | --- | --- |
| 主页 | /static/ | 查看程序版本、监听地址、运行时长 |
| 设置 | /static/settings | 编辑并保存基础配置 |

## 快速开始

### 环境要求

- Go 1.25+
- Node.js ^20.19.0 或 >= 22.12.0
- pnpm

### 1. 构建前端

```bash
cd frontend
pnpm install
pnpm build
```

### 2. 启动后端

```bash
go run main.go
```

如需构建二进制：

```bash
go build -o tank-tool .
./tank-tool
```

如需跨平台构建：

```bash
sh build.sh
```

默认访问地址：

```text
http://localhost:8888/static/
```

## 配置说明

默认配置文件位于 config/config.toml。首次运行时如果配置不存在，程序会自动生成默认配置，并在交互式终端下补问缺失项。

当前脚手架保留以下配置分组：

- web_config：监听主机和端口
- node_config：共享令牌
- auth_config：访问令牌

设置页会直接读取并保存这些字段。

## 作为脚手架改项目名

这个仓库后续大概率会被拿去孵化新项目，因此第一步通常就是把当前默认的项目名替换掉。

当前脚手架默认将命名拆成三层：

- 程序展示名称：管区辅助工具
- 应用英文名：tank-helper
- 项目英文简称：tank-tool

### 当前脚手架修改范围

为了避免只改一半导致命名错位，建议把脚手架修改范围理解为下面三层：

#### 1. 展示层命名

- 中文展示名称：当前为 `管区辅助工具`
- 应用英文名：当前为 `tank-helper`
- 这两项主要影响页面标题、接口返回的站点信息和前端品牌区
- 当前对应文件：`internal/app/meta.go`、`frontend/index.html`、`frontend/src/components/AppShell.vue`、`frontend/src/pages/Home.vue`

#### 2. 项目层命名

- Go 模块名：当前为 `tank-tool`
- 项目英文简称：当前为 `tank-tool`
- 构建产物前缀：当前为 `tank-tool`
- 这三项在当前脚手架里视为同一组标识，默认应保持一致
- 当前对应文件：`go.mod`、`build.sh`、所有 `tank-tool/...` Go 导入路径

#### 3. 静态资源与文档层

- 浏览器标题、品牌区文案、README 说明文档、CLAUDE.md
- 前端公共资源命名与引用
- 这部分虽然不参与 Go 编译，但如果不一起改，最容易留下旧脚手架痕迹

### 当前约定的实际值

当前仓库已经明确采用以下组合，后续修改时请以此为基线：

- [go.mod](go.mod) 中的 Go 模块名是 `tank-tool`
- [build.sh](build.sh) 中的构建产物前缀是 `tank-tool`
- [internal/app/meta.go](internal/app/meta.go) 中的 `AppName` 是 `tank-helper`
- [internal/app/meta.go](internal/app/meta.go) 中的 `AppDisplayName` 是 `管区辅助工具`

这意味着：

- `tank-tool` 代表项目简称、Go 模块名和构建产物名
- `tank-helper` 代表应用英文名，不等于 Go 模块名
- `管区辅助工具` 代表中文展示名称，不等于二进制名

### 修改时的联动规则

#### 只改展示名称

如果你只想改界面展示和应用名称，而不改项目简称，那么只需要联动修改：

- `internal/app/meta.go`
- `frontend/index.html`
- `frontend/src/components/AppShell.vue`
- `frontend/src/pages/Home.vue`
- `README.md`

这种情况下，不要改 `go.mod`、`build.sh` 和 Go import 路径。

#### 连 Go 模块名一起改

如果你要把 `tank-tool` 这组项目简称一起改掉，那么必须一次性联动修改：

- `go.mod`
- `build.sh`
- 所有 `tank-tool/...` 导入路径
- README 和 CLAUDE 中涉及模块名、构建命令、示例导入的内容

不要只改 `go.mod` 或只改 `build.sh`。这两类修改如果分开做，会出现编辑器、构建产物和源码导入彼此不一致的问题。

#### 前端构建产物与资源

- 不要手改 `frontend/dist/`
- 改完前端源码后重新执行 `pnpm build`
- 如旧模板资源已经不再引用，应一并清理，避免保留无效的脚手架残留

如果你要让 AI 直接帮你改名，建议一次明确给出这三类名称：

- Go 模块名 / 项目英文简称：例如 `acme-admin`
- 应用英文名：例如 `acme-helper`
- 对外展示名：例如 `Acme Admin Console`
- 构建产物名：例如 `acme-admin`

### 最少需要修改的文件

#### 1. 后端与构建相关

- [go.mod](go.mod)：修改 `module tank-tool`
- [internal/app/meta.go](internal/app/meta.go)：修改 `AppName`、`AppDisplayName`
- [build.sh](build.sh)：修改 `APP_NAME`
- [README.md](README.md)：同步替换文档中的旧项目名

#### 2. 前端展示相关

- [frontend/index.html](frontend/index.html)：修改浏览器标题 `title`
- [frontend/src/components/AppShell.vue](frontend/src/components/AppShell.vue)：修改页头品牌名、首页描述文案

#### 3. Go 导入路径相关

如果你连 Go 模块名也要一起改，那么所有 `tank-tool/...` 导入路径都要同步修改。

当前这类文件主要包括：

- [main.go](main.go)
- [cmd/wire.go](cmd/wire.go)
- [cmd/wire_gen.go](cmd/wire_gen.go)
- [internal/app/app.go](internal/app/app.go)
- [internal/api/site_api.go](internal/api/site_api.go)
- [internal/api/settings_api.go](internal/api/settings_api.go)
- [internal/api/providers/wire_set.go](internal/api/providers/wire_set.go)
- [internal/config/config.go](internal/config/config.go)
- [internal/core/ds/providers/wire_set.go](internal/core/ds/providers/wire_set.go)
- [pkg/api/api.go](pkg/api/api.go)
- [pkg/common/common.go](pkg/common/common.go)
- [pkg/core/tx/data_source.go](pkg/core/tx/data_source.go)
- [pkg/core/tx/manager.go](pkg/core/tx/manager.go)
- [pkg/core/tx/transaction.go](pkg/core/tx/transaction.go)
- [pkg/models/model.go](pkg/models/model.go)
- [pkg/repository/repo.go](pkg/repository/repo.go)
- [pkg/service/service.go](pkg/service/service.go)

### 不建议手改的内容

- 不要手改 `frontend/dist/`，前端源码改完后重新执行 `pnpm build`
- 不要直接手改 Wire 生成产物的业务逻辑；如果变更了依赖装配，优先修改 [cmd/wire.go](cmd/wire.go)，再重新生成 [cmd/wire_gen.go](cmd/wire_gen.go)

### 推荐让 AI 执行的改名任务

你可以直接把下面这段话给 AI：

```text
请把这个脚手架项目改名。

目标信息：
1. Go 模块名改为：my-new-app
2. 应用英文名改为：my-new-helper
3. 程序显示名称改为：My New App
4. 构建产物名改为：my-new-app

要求：
1. 同步修改 go.mod、internal/app/meta.go、build.sh、frontend/index.html、frontend/src/components/AppShell.vue、README.md
2. 把所有 Go 源码中的 tank-tool/... 导入路径改成 my-new-app/...
3. 不要手改 frontend/dist，改完前端源码后重新执行 pnpm build
4. 最后执行 go build -buildvcs=false ./... 和 frontend 的 pnpm build 验证
```

### 改名后的验证步骤

完成改名后，至少验证下面两步：

```bash
cd frontend && pnpm build
cd .. && go build -buildvcs=false ./...
```

如果你还改了构建产物名，再补一次：

```bash
sh build.sh
```

## 扩展建议

如果要在这个脚手架上继续开发，推荐按下面的顺序扩展：

1. 在 internal/api 下新增业务 API，并在 cmd/wire.go 与 cmd/wire_gen.go 中补充注入链。
2. 在 frontend/src/pages 下新增页面，并在 frontend/src/router/index.ts 中注册路由。
3. 如果需要新增持久化模型，再补 Repository、Service 和 Wire 依赖装配。

- 这是一个 Go + Vue 3 单仓库
- 前端位于 frontend/
- 后端主代码位于 internal/
- 依赖注入入口位于 cmd/wire.go
- 后端通过 main.go 的 go:embed 打包前端产物

常用开发命令：

```bash
# 前端开发
cd frontend && pnpm dev

# 前端构建
cd frontend && pnpm build

# Wire 生成
cd cmd && go run -mod=mod github.com/google/wire/cmd/wire

# 后端运行
go run main.go
```

更细的后端分层规范和约束说明，请继续阅读 CLAUDE.md、pkg/core/tx/README.md、pkg/repository/README.md 和 pkg/service/README.md。
