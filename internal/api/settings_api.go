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
	security      *AdminSecurity
}

func NewSettingsApi(configManager *config.ApplicationConfigManager, security *AdminSecurity) *SettingsApi {
	return &SettingsApi{configManager: configManager, security: security}
}

func (a *SettingsApi) Register(router *gin.RouterGroup) {
	settings := router.Group("/settings")
	settings.Use(a.security.RequireAdmin, a.security.VerifyOrigin)
	settings.GET("", a.GetSettings)
	settings.PUT("", a.UpdateSettings)
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
		c.JSON(http.StatusBadRequest, common.F[any](400, "参数错误: "+err.Error()))
		return
	}
	if err := a.configManager.Save(&req); err != nil {
		c.JSON(http.StatusInternalServerError, common.F[any](500, "保存配置失败: "+err.Error()))
		return
	}
	// Refresh the current administrator cookie so a changed session lifetime
	// takes effect on this response instead of waiting for the next request.
	a.security.refreshSessionCookie(c)
	var result any = a.configManager.GetConfig()
	c.JSON(http.StatusOK, common.S(&result))
}
