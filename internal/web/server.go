package web

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/web/handler"
	"github.com/jayce/btc-trader/internal/web/ws"
	"go.uber.org/zap"
)

// Server is the Dashboard HTTP server.
type Server struct {
	engine *gin.Engine
	addr   string
	hub    *ws.Hub
	logger *zap.Logger
}

// NewServer creates and configures the dashboard server.
func NewServer(deps *handler.Deps, logger *zap.Logger) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// Middleware
	engine.Use(handler.ZapLogger(logger), handler.Recovery(logger))
	engine.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		AllowMethods:    []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:    []string{"Content-Type"},
		AllowWebSockets: true,
		MaxAge:          12 * time.Hour,
	}))

	// WebSocket hub
	hub := ws.NewHub(logger.Named("ws"))

	// REST handlers
	h := handler.New(deps, logger.Named("api"))

	api := engine.Group("/api/v1")
	{
		api.GET("/overview", h.GetOverview)
		api.GET("/positions", h.GetPositions)
		api.GET("/positions/:symbol", h.GetPosition)
		api.GET("/orders", h.GetOrders)
		api.GET("/orders/active", h.GetActiveOrders)
		api.GET("/orders/:id", h.GetOrder)
		api.GET("/trades", h.GetTrades)
		api.GET("/signals", h.GetSignals)
		api.GET("/snapshots", h.GetSnapshots)
		api.GET("/klines", h.GetKlines)
		api.GET("/risk/status", h.GetRiskStatus)
		api.GET("/strategy/status", h.GetStrategyStatus)
		api.GET("/config", h.GetConfig)
		api.GET("/ticker/:symbol", h.GetTicker)
		api.POST("/backtest", h.RunBacktest)
		api.GET("/backtest/strategies", h.GetStrategies)
		api.GET("/ws", hub.HandleWebSocket)
	}

	// Embedded frontend (production builds only)
	registerStaticFiles(engine)

	return &Server{
		engine: engine,
		addr:   deps.Config.Dashboard.Addr,
		hub:    hub,
		logger: logger,
	}
}

// Hub returns the WebSocket hub for bridge wiring.
func (s *Server) Hub() *ws.Hub {
	return s.hub
}

// Run starts the HTTP server and blocks until ctx is canceled.
func (s *Server) Run(ctx context.Context) error {
	// Start WebSocket hub
	go s.hub.Run(ctx)

	srv := &http.Server{Addr: s.addr, Handler: s.engine}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()

	s.logger.Info("dashboard server starting", zap.String("addr", s.addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
