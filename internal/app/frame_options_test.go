package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFrameOptionsMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		allowIframe  bool
		wantFrameOpt string
	}{
		{name: "allowed by default", allowIframe: true},
		{name: "disabled", allowIframe: false, wantFrameOpt: "DENY"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(frameOptionsMiddleware(test.allowIframe))
			engine.GET("/", func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			engine.ServeHTTP(recorder, request)

			if got := recorder.Header().Get("X-Frame-Options"); got != test.wantFrameOpt {
				t.Fatalf("X-Frame-Options = %q, want %q", got, test.wantFrameOpt)
			}
		})
	}
}
