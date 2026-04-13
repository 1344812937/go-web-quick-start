# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

**Prerequisites:** Go 1.25+, Node.js (^20.19.0 || >=22.12.0), pnpm

**Frontend must be built before backend** (Go embeds `frontend/dist/` via `//go:embed`):

```bash
# Frontend
cd frontend && pnpm install && pnpm build

# Wire code generation (after changing cmd/wire.go)
cd cmd && go run -mod=mod github.com/google/wire/cmd/wire

# Build backend
go build -o neko-tool .

# Cross-platform build (Linux/Windows/macOS amd64 → build/)
./build.sh
```

Frontend dev server: `cd frontend && pnpm dev`

App runs on `localhost:8888` by default (configured in `config/config.toml`, auto-created on first run).

## Architecture

Go + Vue3 monorepo. Module name: `neko-tool`.

### Backend Layers

- **Wire DI** (`cmd/wire.go`): `InitializeApp()` builds `ApplicationHolder` ← `ApplicationConfigManager`
- **App** (`internal/app/`): `ApplicationHolder` manages lifecycle; `AppWebManager` runs Gin server with embedded SPA
- **API** (`internal/api/`): Route handlers with panic-based error signaling (`ServicePanic`)
- **Config** (`internal/config/`): TOML config from `./config/config.toml` with auto-creation and defaults
- **Tasks** (`internal/tasks/`): Cron jobs via `robfig/cron/v3`

### pkg/ — Reusable Framework

- **`pkg/api/`**: `IApi` 接口（业务 API 自注册路由）、`BaseApi` 基类（内含 `DeferPanicHandler` 方法，统一 panic 恢复）
- **`pkg/common/`**: `R[T]` (API response), `Page[T]` (pagination), `ServicePanic` (error flow)
- **`pkg/models/`**: `BaseModel` with snowflake ID, soft delete (`Valid` field), auto-timestamps
- **`pkg/repository/`**: Generic `BaseRepository[T]` — CRUD with soft delete, transaction binding via `WithScope()`/`WithDb()`
- **`pkg/service/`**: Generic `BaseService[T]` — transaction-aware, `JoinTx()`/`NewTx()` for scope management
- **`pkg/core/tx/`**: `TransactionScope` with parent-child nesting, `MultiDataSource` for multiple DBs, context propagation via `FromCtx()`/`GetScope()`
- **`pkg/until/`**: Logrus logging (log4j format, daily rotation to `./logs/app/`), snowflake ID generator

### Frontend

Vue 3 + TypeScript + Vite 7 + Element Plus, base path `/static/`. Served by Go via `embed.FS` with custom MIME mapping and SPA fallback (non-static routes → `index.html`). No lint or test configuration.

### Key Patterns

- **API responses**: Always use `R[T]` — `common.S[T]()` for success, `common.F[T]()` for failure
- **Error signaling**: Services panic with `ServicePanic`; API layer recovers and converts to `R[T]`
- **Transactions**: `TransactionScope` propagated via `context.Context`; child failure cascades to parent
- **Soft delete**: All entities use `Valid` field (not GORM's default `DeletedAt`), default sort by `sort asc`
- **IDs**: 16-digit snowflake IDs (uint64), auto-generated in `BaseModel.BeforeCreate` hook

### Repository 层编写规范

业务 Repository 位于 `internal/repository/`，继承 `pkg/repository/BaseRepository[T]`。

**结构定义**：
- 内嵌 `*repository.BaseRepository[models.XxxModel]`，直接复用基类的 CRUD、分页、事务绑定等能力
- 不要重复定义 `BaseRepository` 已有的接口方法

```go
type XxxRepository struct {
	*repository.BaseRepository[models.XxxModel]
}
```

**构造函数**：
- 接收 `*tx.DataSource` 参数（由 Wire 注入）
- 调用 `repository.NewBaseRepository[T](ds)` 初始化基类
- 必须调用 `InitializeRepository()` 完成 AutoMigrate
- 函数命名：`NewXxxRepository`

```go
func NewXxxRepository(ds *tx.DataSource) *XxxRepository {
	repo := &XxxRepository{
		BaseRepository: repository.NewBaseRepository[models.XxxModel](ds),
	}
	repo.InitializeRepository()
	return repo
}
```

**自定义方法**：
- 基类 `DeleteById(id uint)` 参数是 `uint`，而项目使用雪花 ID（`uint64`）。需要 `uint64` 参数的删除方法时，命名为 `SoftDeleteById` 避免与接口签名冲突
- 自定义查询通过 `r.Where(...)` 构建，返回 `error`
- 需要事务感知时，Service 层通过 `WithScope`/`WithDb` 获取绑定副本，Repository 自身不处理事务

**Wire 注入**：
- 在 `cmd/wire.go` 的 `wire.Build` 中注册 `NewXxxRepository`
- 依赖链：`providers.GetPrimaryDataSource` → `NewXxxRepository` → `NewXxxService` → `NewXxxApi`

### Service 层编写规范

业务 Service 位于 `internal/service/`，继承 `pkg/service/BaseService[T]`。

**结构定义**：
- 内嵌 `*service.BaseService[models.XxxModel]`，复用基类的事务管理、CRUD 查询能力
- 如需调用 Repository 自定义方法，额外保存业务 Repository 引用
- 包级别日志变量：`var xxxLog = until.Log`

```go
type XxxService struct {
	*service.BaseService[models.XxxModel]
	repo *repository.XxxRepository // 可选：仅在需要调用自定义 repo 方法时保留
}
```

**构造函数**：
- 接收对应的业务 Repository 指针（由 Wire 注入）
- 通过 `service.NewBaseService[T](repo)` 初始化基类
- 函数命名：`NewXxxService`

```go
func NewXxxService(repo *repository.XxxRepository) *XxxService {
	return &XxxService{
		BaseService: service.NewBaseService[models.XxxModel](repo),
		repo:        repo,
	}
}
```

**业务方法**：
- 方法签名第一个参数为 `ctx context.Context`（用于事务传播）
- 返回值为 `(T, error)` 或 `error`，不使用 panic
- 通过 `s.Exec(ctx)` 获取事务感知的执行器，不直接调用 `s.repo` 的 CRUD 方法
- 错误时记录日志并返回 error，由 API 层决定响应格式

```go
func (s *XxxService) CreateXxx(ctx context.Context, name string) (models.XxxModel, error) {
	entity := models.XxxModel{Name: name}
	if err := s.Exec(ctx).Create(&entity); err != nil {
		xxxLog.Error("创建失败: ", err)
		return entity, err
	}
	return entity, nil
}
```

**事务使用**：
- `newTx(ctx)`（私有方法）: 创建新事务域，调用方必须 `defer` 返回的第三个值（deliver 函数）
- `joinTx(ctx)`（私有方法）: 加入已有事务域，不持有交付权，但同样需要 `defer` 返回的第三个值
- 事务方法仅限 Service 内部调用，外部（如 API 层）不应直接操作事务
- `Exec(ctx)` 方法自动感知上下文中的事务，返回绑定后的 `IExecutor`

```go
func (s *XxxService) BatchOperation(ctx context.Context) error {
	ctx, _, deliver := s.newTx(ctx)
	defer deliver()
	// 后续操作自动在事务内执行
	return nil
}
```

### API 层编写规范

业务 API 位于 `internal/api/`，负责路由处理和请求/响应转换。每个 API 实现 `pkg/api.IApi` 接口，通过 `Register()` 方法自行注册路由。

**结构定义**：
- 内嵌 `pkgApi.BaseApi`，复用基类的 `DeferPanicHandler` 等通用能力
- 持有业务 Service 指针作为依赖
- 包级别日志变量：`var xxxLog = until.Log`
- 编译期接口检查：`var _ pkgApi.IApi = (*XxxApi)(nil)`

```go
import pkgApi "neko-tool/pkg/api"

var _ pkgApi.IApi = (*XxxApi)(nil)

type XxxApi struct {
	pkgApi.BaseApi
	xxxService *service.XxxService
}
```

**构造函数**：
- 接收对应的业务 Service 指针（由 Wire 注入）
- 函数命名：`NewXxxApi`

```go
func NewXxxApi(svc *service.XxxService) *XxxApi {
	return &XxxApi{xxxService: svc}
}
```

**Register 方法**（实现 `IApi` 接口）：
- 在此方法中按 RESTful 风格注册自身路由
- 路径参数使用 `:param` 格式，通过 `c.Param("param")` 获取

```go
func (a *XxxApi) Register(router *gin.RouterGroup) {
	router.GET("/xxx", a.List)
	router.POST("/xxx", a.Create)
	router.DELETE("/xxx/:id", a.Delete)
}
```

**Handler 方法**：
- 签名统一为 `func (a *XxxApi) MethodName(c *gin.Context)`
- 第一行 `defer a.DeferPanicHandler(c)`，通过内嵌的 `BaseApi` 调用，panic 时自动捕获并写回 JSON 响应
- 使用 `c.Request.Context()` 传递上下文给 Service
- 所有 HTTP 状态码均为 200，业务状态通过 `R[T]` 的 `Code` 字段区分
- 成功：`common.S[any](data)`，失败：`common.F[any](code, msg)`
- 参数校验失败 code=400，服务异常 code=500

```go
func (a *XxxApi) List(c *gin.Context) {
	defer a.DeferPanicHandler(c)

	list, err := a.xxxService.List(c.Request.Context())
	if err != nil {
		xxxLog.Error("查询列表失败: ", err)
		c.JSON(http.StatusOK, common.F[any](500, "查询列表失败"))
		return
	}
	var data any = list
	c.JSON(http.StatusOK, common.S(&data))
}

func (a *XxxApi) Create(c *gin.Context) {
	defer a.DeferPanicHandler(c)

	var req CreateXxxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, common.F[any](400, "参数错误: "+err.Error()))
		return
	}

	entity, err := a.xxxService.CreateXxx(c.Request.Context(), req.Name)
	if err != nil {
		c.JSON(http.StatusOK, common.F[any](500, "创建失败"))
		return
	}
	var data any = entity
	c.JSON(http.StatusOK, common.S(&data))
}
```

**`DeferPanicHandler` 机制**（定义在 `pkg/api/api.go`，`BaseApi` 的方法）：
- 接收 `*gin.Context`，panic 时自动构造错误响应并通过 `c.JSON` 写回客户端
- 捕获 `ServicePanic` → 转为 `common.F[any](code, msg)`
- 捕获其他 `error` → 转为 500 错误
- 捕获未知类型 → 转为 500 未知异常

### Wire 注入完整流程

新增业务模块时，在 `cmd/wire.go` 的 `wire.Build` 中注册构造函数，并在 `internal/api/providers/wire_set.go` 的 `ProvideApis` 中添加新 API：

```go
// cmd/wire.go
wire.Build(
	// ... 基础设施 ...
	dsProviders.NewMultiDataSource,
	dsProviders.GetPrimaryDataSource,

	// Repository → Service → API
	internalRepo.NewXxxRepository,
	internalSvc.NewXxxService,
	api.NewXxxApi,
	apiProviders.ProvideApis,    // 聚合所有 IApi

	// ... 应用层 ...
	app.NewAppWebManager,
	app.NewApplicationHolder,
)
```

```go
// internal/api/providers/wire_set.go
func ProvideApis(projectApi *api.ProjectApi, xxxApi *api.XxxApi) []pkgApi.IApi {
	return []pkgApi.IApi{projectApi, xxxApi}
}
```

`AppWebManager` 接收 `[]pkgApi.IApi` 切片，在 `RegisterRouter()` 中遍历调用 `Register(apiGroup)` 完成路由注册，无需手动修改 `AppWebManager`。

### Wire Provider 文件规范

所有需要交给 Wire 注入的 Provider 函数，存放在所属模块目录下的 `providers/wire_set.go` 文件中。目录结构为 `<模块目录>/providers/wire_set.go`。

现有示例：
- `internal/core/ds/providers/wire_set.go` — 数据源相关 Provider（`NewMultiDataSource`、`GetPrimaryDataSource`）
- `internal/api/providers/wire_set.go` — API 层聚合 Provider（`ProvideApis`）

在 `cmd/wire.go` 中通过别名导入对应的 providers 包：
```go
dsProviders "neko-tool/internal/core/ds/providers"
apiProviders "neko-tool/internal/api/providers"
```

新增模块需要自定义 Provider 时，在模块目录下创建 `providers/wire_set.go`，保持 Provider 函数集中管理。

## Language

Project comments, commit messages, and documentation are in Chinese (中文).
