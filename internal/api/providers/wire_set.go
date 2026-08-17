package providers

import (
	"github.com/1344812937/go-web-quick-start/internal/api"
	pkgApi "github.com/1344812937/go-web-quick-start/pkg/api"
)

func ProvideApis(siteApi *api.SiteApi, settingsApi *api.SettingsApi, adminApi *api.AdminApi, gatewayApi *api.GatewayManagementApi) []pkgApi.IApi {
	return []pkgApi.IApi{siteApi, settingsApi, adminApi, gatewayApi}
}

func ProvideRootApi(relayApi *api.OpenAIRelayApi) pkgApi.IRootApi {
	return relayApi
}
