//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.
package cmd

import (
	"github.com/1344812937/go-web-quick-start/internal/api"
	apiProviders "github.com/1344812937/go-web-quick-start/internal/api/providers"
	"github.com/1344812937/go-web-quick-start/internal/app"
	"github.com/1344812937/go-web-quick-start/internal/config"
	dsProviders "github.com/1344812937/go-web-quick-start/internal/core/ds/providers"
	"github.com/1344812937/go-web-quick-start/internal/gateway"

	"github.com/google/wire"
)

func InitializeApp() *app.ApplicationHolder {
	wire.Build(
		// 配置
		config.NewApplicationConfigManager,
		dsProviders.NewMultiDataSource,
		dsProviders.GetPrimaryDataSource,
		gateway.NewStore,
		gateway.NewAdminAuthService,
		gateway.NewManagementService,
		gateway.NewClientAccessService,
		gateway.NewTokenEstimator,
		gateway.NewRouter,
		gateway.NewRelayService,

		// API
		api.NewSiteApi,
		api.NewAdminSecurity,
		api.NewAdminApi,
		api.NewGatewayManagementApi,
		api.NewOpenAIRelayApi,
		api.NewSettingsApi,
		apiProviders.ProvideApis,
		apiProviders.ProvideRootApi,

		// 应用层
		app.NewAppWebManager,
		app.NewApplicationHolder,
	)
	return nil
}
