package api

import (
	"github.com/1344812937/go-web-quick-start/internal/config"
	pkgApi "github.com/1344812937/go-web-quick-start/pkg/api"
	"github.com/1344812937/go-web-quick-start/pkg/common"
	"net/http"

	gin "github.com/gin-gonic/gin"
)

type SettingsApi struct {
	pkgApi.BaseApi
	configManager *config.ApplicationConfigManager
}

func NewSettingsApi(configManager *config.ApplicationConfigManager) *SettingsApi {
	return &SettingsApi{configManager: configManager}
}

func (a *SettingsApi) Register(router *gin.RouterGroup) {
	router.GET("/settings", a.GetSettings)
	router.PUT("/settings", a.UpdateSettings)
}

func (a *SettingsApi) GetSettings(c *gin.Context) {
	defer a.DeferPanicHandler(c)

	var result any = a.configManager.GetConfig()
	c.JSON(http.StatusOK, common.S(&result))
}

func (a *SettingsApi) UpdateSettings(c *gin.Context) {
	defer a.DeferPanicHandler(c)

	var req config.ApplicationConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, common.F[any](400, "参数错误: "+err.Error()))
		return
	}
	if err := a.configManager.Save(&req); err != nil {
		c.JSON(http.StatusOK, common.F[any](500, "保存配置失败: "+err.Error()))
		return
	}
	var result any = a.configManager.GetConfig()
	c.JSON(http.StatusOK, common.S(&result))
}
