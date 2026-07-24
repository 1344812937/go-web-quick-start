# Go + Vue 快速功能开发脚手架

这是一个面向快速功能搭建的 Go + Vue 3 单仓库项目，目标是用尽量少的装配工作完成结构简单、便于修改的 Web 应用。它适合功能展示、原型验证、内部工具和其他轻量场景，也可以作为 Agent 持续开发的稳定起点。

仓库地址：[1344812937/go-web-quick-start](https://github.com/1344812937/go-web-quick-start)

## 项目包含什么

- Gin Web 服务与内嵌前端资源
- Wire 依赖注入
- GORM 与 SQLite 基础能力
- Vue 3、Vue Router、Vite、Element Plus
- 支持递归多级的功能菜单
- 菜单与页面路由统一注册
- 本地配置、站点信息和令牌示例
- 项目身份生成、校验与批量重命名工具
- 面向 Agent 的需求模板、开发规范和 SUPOS 工业主题规则

Go 二进制会嵌入 `frontend/dist/`，发布时不需要单独部署前端目录。项目名称、模块路径、版本、描述、令牌前缀和 favicon 统一维护在 `project.json`。

## 使用 Agent 开发

让 Agent 在仓库根目录工作，并在开始前明确要求它读取 [AGENTS.md](AGENTS.md)。该文档包含环境检测、安装失败处理、分支建议、菜单注册、主题、测试、安全和打包规范。

推荐的任务描述：

```text
请先读取 AGENTS.md，并根据任务读取 docs/prompts 下对应模板。
在当前项目基础上实现 <功能目标>。
新功能需要直接挂载到 <父菜单或菜单分组>，页面风格使用默认 SUPOS 工业主题。
请完成构建、测试和实际页面验证，不要覆盖工作区已有修改。
```

需求还不完整时，可从以下模板选择：

- [功能开发模板](docs/prompts/feature-request.md)
- [模块开发模板](docs/prompts/module-request.md)
- [页面调整模板](docs/prompts/page-change-request.md)

模板不是开发前置条件。目标、范围和验收方式已经明确时，Agent 应直接实施，只对会改变实现的缺失信息提问。

## 功能菜单开发约定

所有面向用户的新功能都应成为功能菜单。新增页面直接登记在 `frontend/src/navigation/index.ts` 的 `navigationGroups` 中，Vue Router 会从同一配置自动生成路由，不需要再编辑独立路由表。

菜单项支持任意实用层级的 `children`。一个可访问页面叶子需要提供：

```ts
{
  key: 'device-monitoring',
  path: '/device-monitoring',
  routeName: 'device-monitoring',
  label: '设备监控',
  icon: Monitor,
  component: () => import('@/pages/DeviceMonitoring.vue'),
}
```

将该对象放到目标父菜单的 `children` 中即可形成多级菜单。菜单采用“配置即显示”，不做角色、权限、租户、隐藏或灰度过滤；登记后直接可见、可访问。菜单搜索会保留父级路径，访问子页面会展开父菜单并生成完整面包屑。

## 项目结构

```text
.
├── cmd/                         # Wire 定义与生成代码
├── docs/
│   ├── design/                  # SUPOS 工业主题规则
│   └── prompts/                 # 功能、模块、页面需求模板
├── frontend/
│   ├── public/                  # 公共静态资源
│   └── src/
│       ├── components/          # Vue 组件
│       ├── navigation/          # 功能菜单与路由注册源
│       └── pages/               # 功能页面
├── internal/                    # 应用、API、配置及基础设施实现
├── pkg/                         # 可复用模型与服务能力
├── tools/projectctl/            # 项目身份管理工具
├── AGENTS.md                    # Agent 开发规范
└── project.json                 # 项目身份清单
```

## 快速开始

环境要求：Go `1.25.4`、Node.js `^20.19.0 || >=22.12.0`、pnpm `10.23.0`。多平台安装和故障处理见 [AGENTS.md](AGENTS.md#快速开始)。

```bash
go mod download
cd frontend
pnpm install --frozen-lockfile
pnpm build
cd ..
go run .
```

后端默认监听 `0.0.0.0:8888`，启动后会调用一次系统默认浏览器并打开 `http://127.0.0.1:8888/static/`。

前后端联调使用两个终端：

```bash
# 终端一，首次运行前需保证 frontend/dist 已生成
go run .

# 终端二
pnpm --dir frontend dev
```

Vite 将 `/api` 代理到 `http://127.0.0.1:8888`。仅启动 Vite 时只能查看前端外观，依赖 API 的页面会提示先启动 Go 服务。

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
./build.sh
```

可使用 `PACKAGE_NAME=my-app ./build.sh` 指定本次产物名。未指定时，脚本优先从具有业务含义的当前分支名生成。

## 页面与 API

| 功能 | 页面或接口 |
| --- | --- |
| 运行总览 | `/static/` |
| 基础设置 | `/static/settings` |
| 站点信息 | `GET /api/site/info` |
| 读取设置 | `GET /api/settings` |
| 保存设置 | `PUT /api/settings` |

首次启动会在 `config/config.toml` 生成本地配置。该目录已被 Git 忽略，不要提交其中的令牌或环境配置。

## 项目个性化

编辑 `project.json` 后执行：

```bash
go run ./tools/projectctl generate
go run ./tools/projectctl check
```

需要同时修改 Go module、内部 import 和生成元数据时：

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

现有 `config/config.toml` 和已有令牌不会被自动迁移。

## Agent 与界面规范

- 开发前检查工作区状态并保留用户修改。
- 新功能页面必须直接挂载到功能菜单。
- 页面开发默认读取 `docs/design/supos-industrial/THEME.md`。
- 当前 UI 只是功能示例，不限制后续业务布局；用户指定其他风格时可重新设计。
- 修改后执行与风险匹配的构建、测试和桌面/移动端检查。
- 系统安装、全局依赖、代理或管理员权限操作必须先取得用户批准。

## 免责声明

本项目按现状提供，主要用于快速开发、功能演示和原型验证，不默认满足生产环境的安全性、稳定性、性能、可用性或合规要求。

部署到正式环境前，使用者应自行完成身份认证、HTTPS、访问控制、输入校验、日志审计、备份恢复和监控告警，并更换示例令牌、默认配置及其他敏感信息。开放 `0.0.0.0` 监听时，应结合防火墙和网络边界限制访问范围。

第三方依赖遵循各自许可证。使用、修改、分发和部署本项目产生的风险及责任由使用者自行评估和承担。
