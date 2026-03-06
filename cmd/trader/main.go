package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jayce/btc-trader/internal/app"
	"github.com/jayce/btc-trader/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	migrateOnly := flag.Bool("migrate", false, "run database migrations and exit")
	flag.Parse()

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	// Init logger
	logger, err := initLogger(cfg.App.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("btc-trader starting",
		zap.String("mode", cfg.App.Mode),
		zap.Bool("testnet", cfg.App.Testnet),
	)

	// Context with graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create trader
	trader, err := app.NewTrader(ctx, cfg, logger)
	if err != nil {
		logger.Fatal("init trader", zap.Error(err))
	}
	defer trader.Shutdown()

	// Migrate-only mode
	if *migrateOnly {
		logger.Info("migrations complete, exiting")
		return
	}

	// Run
	if err := trader.Run(ctx); err != nil {
		if err == context.Canceled {
			logger.Info("trader stopped by signal")
		} else {
			logger.Fatal("trader error", zap.Error(err))
		}
	}
}

func initLogger(level string) (*zap.Logger, error) {
	lvl := zapcore.InfoLevel
	switch level {
	case "debug":
		lvl = zapcore.DebugLevel
	case "warn":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	}

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(lvl),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return cfg.Build()
}
