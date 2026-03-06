//go:build !dev

package web

import (
	"embed"
	"io/fs"
	"net/http"

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
	engine.NoRoute(gin.WrapH(fileServer))
}
