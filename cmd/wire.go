//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.
package cmd

import (
	"tank-tool/internal/api"
	apiProviders "tank-tool/internal/api/providers"
	"tank-tool/internal/app"
	"tank-tool/internal/config"

	"github.com/google/wire"
)

func InitializeApp() *app.ApplicationHolder {
	wire.Build(
		// 配置
		config.NewApplicationConfigManager,

		// API
		api.NewSiteApi,
		api.NewSettingsApi,
		apiProviders.ProvideApis,

		// 应用层
		app.NewAppWebManager,
		app.NewApplicationHolder,
	)
	return nil
}
