package api

import (
	"net/http"
	"tank-tool/internal/app"
	"tank-tool/internal/config"
	pkgApi "tank-tool/pkg/api"
	"tank-tool/pkg/common"
	"time"

	gin "github.com/gin-gonic/gin"
)

type SiteApi struct {
	pkgApi.BaseApi
	configManager *config.ApplicationConfigManager
}

type SiteInfo struct {
	AppName     string `json:"appName"`
	Title       string `json:"title"`
	Version     string `json:"version"`
	BasePath    string `json:"basePath"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	StartedAt   string `json:"startedAt"`
	RunningFor  string `json:"runningFor"`
	Description string `json:"description"`
}

func NewSiteApi(configManager *config.ApplicationConfigManager) *SiteApi {
	return &SiteApi{configManager: configManager}
}

func (a *SiteApi) Register(router *gin.RouterGroup) {
	router.GET("/site/info", a.GetSiteInfo)
}

func (a *SiteApi) GetSiteInfo(c *gin.Context) {
	defer a.DeferPanicHandler(c)

	cfg := a.configManager.GetConfig()
	data := SiteInfo{
		AppName:     app.AppName,
		Title:       app.AppDisplayName,
		Version:     app.AppVersion,
		BasePath:    app.StaticBasePath,
		Host:        cfg.WebConfig.Host,
		Port:        cfg.WebConfig.Port,
		StartedAt:   app.StartedAt.Format(time.RFC3339),
		RunningFor:  time.Since(app.StartedAt).Round(time.Second).String(),
		Description: "一个保留主页与设置能力的 Go + Vue 3 单仓库脚手架。",
	}
	var result any = data
	c.JSON(http.StatusOK, common.S(&result))
}
