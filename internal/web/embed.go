//go:build !dev

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var distFS embed.FS

func registerStaticFiles(engine *gin.Engine) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	engine.NoRoute(func(c *gin.Context) {
		// API 路由未命中时保持 404，不要回退到 index.html。
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Status(http.StatusNotFound)
			return
		}
		// 命中真实静态资源（js/css/assets 等）就直接返回。
		reqPath := strings.TrimPrefix(c.Request.URL.Path, "/")
		if reqPath != "" {
			if f, err := sub.Open(reqPath); err == nil {
				f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		// SPA fallback：未知路径（如 /backtest）回退到 index.html，交给前端路由。
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
