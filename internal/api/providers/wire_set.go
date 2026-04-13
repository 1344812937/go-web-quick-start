package providers

import (
	"tank-tool/internal/api"
	pkgApi "tank-tool/pkg/api"
)

func ProvideApis(siteApi *api.SiteApi, settingsApi *api.SettingsApi) []pkgApi.IApi {
	return []pkgApi.IApi{siteApi, settingsApi}
}
