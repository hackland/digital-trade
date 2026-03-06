//go:build dev

package web

import "github.com/gin-gonic/gin"

// In dev mode, the frontend is served by Vite dev server.
// No static files are embedded.
func registerStaticFiles(_ *gin.Engine) {}
