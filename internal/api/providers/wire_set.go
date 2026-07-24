package providers

import (
	"github.com/1344812937/go-web-quick-start/internal/api"
	pkgApi "github.com/1344812937/go-web-quick-start/pkg/api"
)

func ProvideApis(siteApi *api.SiteApi, settingsApi *api.SettingsApi) []pkgApi.IApi {
	return []pkgApi.IApi{siteApi, settingsApi}
}
