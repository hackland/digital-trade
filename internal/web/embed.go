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
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if f, err := sub.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// Unknown path (e.g. /backtest, a Vue Router history-mode route): fall
		// back to index.html so the client-side router can render it, instead
		// of 404ing on a direct navigation/refresh to a non-root URL.
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
