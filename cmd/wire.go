//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.
package cmd

import (
	"github.com/1344812937/go-web-quick-start/internal/api"
	apiProviders "github.com/1344812937/go-web-quick-start/internal/api/providers"
	"github.com/1344812937/go-web-quick-start/internal/app"
	"github.com/1344812937/go-web-quick-start/internal/config"

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
