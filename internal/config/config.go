package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	App       AppConfig       `mapstructure:"app"`
	Exchange  ExchangeConfig  `mapstructure:"exchange"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Strategy  StrategyConfig  `mapstructure:"strategy"`
	Risk      RiskConfig      `mapstructure:"risk"`
	Dashboard DashboardConfig `mapstructure:"dashboard"`
	Snapshot  SnapshotConfig  `mapstructure:"snapshot"`
	Telegram  TelegramConfig  `mapstructure:"telegram"`
}

type TelegramConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`
	ChatID  string `mapstructure:"chat_id"`
}

type AppConfig struct {
	Name     string `mapstructure:"name"`
	Mode     string `mapstructure:"mode"`      // live | paper | backtest
	LogLevel string `mapstructure:"log_level"` // debug | info | warn | error
	Testnet  bool   `mapstructure:"testnet"`
}

type ExchangeConfig struct {
	Name           string   `mapstructure:"name"`
	MarketType     string   `mapstructure:"market_type"` // spot | usdt_futures
	Symbols        []string `mapstructure:"symbols"`
	KlineIntervals []string `mapstructure:"kline_intervals"`
	APIKey         string   `mapstructure:"api_key"`
	SecretKey      string   `mapstructure:"secret_key"`
}

type DatabaseConfig struct {
	DSN             string        `mapstructure:"dsn"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

type StrategyConfig struct {
	Name   string                 `mapstructure:"name"`
	Config map[string]interface{} `mapstructure:"config"`
}

type RiskConfig struct {
	AllocPct             float64       `mapstructure:"alloc_pct"` // 每笔交易使用可用余额的比例 (0.0-1.0)
	MaxPositionSizeBTC   float64       `mapstructure:"max_position_size_btc"`
	MaxPositionSizeETH   float64       `mapstructure:"max_position_size_eth"`
	MaxPositionPctAcct   float64       `mapstructure:"max_position_pct_account"`
	DefaultStopLossPct   float64       `mapstructure:"default_stop_loss_pct"`
	DefaultTakeProfitPct float64       `mapstructure:"default_take_profit_pct"`
	ATRStopMultiplier    float64       `mapstructure:"atr_stop_multiplier"` // SL = entry - ATR * multiplier (0 = disabled)
	ATRTPMultiplier      float64       `mapstructure:"atr_tp_multiplier"`   // TP = entry + ATR * multiplier (0 = disabled)
	TrailingStopEnabled  bool          `mapstructure:"trailing_stop_enabled"`
	TrailingStopPct      float64       `mapstructure:"trailing_stop_pct"`
	MaxDailyLossUSDT     float64       `mapstructure:"max_daily_loss_usdt"`
	MaxDailyLossPct      float64       `mapstructure:"max_daily_loss_pct"`
	MaxDailyTrades       int           `mapstructure:"max_daily_trades"`
	MaxDrawdownPct       float64       `mapstructure:"max_drawdown_pct"`
	DrawdownCooldownMins int           `mapstructure:"drawdown_cooldown_mins"`
	MinOrderSizeUSDT     float64       `mapstructure:"min_order_size_usdt"`
	MaxSlippagePct       float64       `mapstructure:"max_slippage_pct"`
	MinTimeBetweenOrders time.Duration `mapstructure:"min_time_between_orders"`

	// Emergency alert: send Telegram warning if unrealized loss from ENTRY exceeds this % (checked every 1m)
	// Protects capital. Alert only, no auto-sell. 0 = disabled.
	EmergencyAlertPct float64 `mapstructure:"emergency_alert_pct"`

	// Peak drawdown alert: send Telegram warning if price drops this % from HIGHEST since entry (checked every 1m)
	// Protects unrealized profit. Alert only, no auto-sell. 0 = disabled.
	PeakDrawdownAlertPct float64 `mapstructure:"peak_drawdown_alert_pct"`
}

type DashboardConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"`
}

type SnapshotConfig struct {
	Interval time.Duration `mapstructure:"interval"`
}

func Load(configPath string) (*Config, error) {
	v := viper.New()

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./configs")
		v.AddConfigPath("/app")
	}

	// Environment variable overrides
	v.SetEnvPrefix("TRADER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Binance API keys from environment
	v.BindEnv("exchange.api_key", "BINANCE_API_KEY")
	v.BindEnv("exchange.secret_key", "BINANCE_SECRET_KEY")
	v.BindEnv("database.dsn", "DB_DSN")
	v.BindEnv("telegram.token", "TELEGRAM_BOT_TOKEN")
	v.BindEnv("telegram.chat_id", "TELEGRAM_CHAT_ID")

	// Defaults
	v.SetDefault("app.name", "btc-trader")
	v.SetDefault("app.mode", "paper")
	v.SetDefault("app.log_level", "info")
	v.SetDefault("database.max_open_conns", 10)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "5m")
	v.SetDefault("dashboard.addr", ":9090")
	v.SetDefault("snapshot.interval", "5m")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.Exchange.Symbols) == 0 {
		return fmt.Errorf("exchange.symbols must not be empty")
	}
	if c.App.Mode == "live" {
		// live 模式强制要求 API Key
		if c.Exchange.APIKey == "" {
			return fmt.Errorf("BINANCE_API_KEY is required in live mode")
		}
		if c.Exchange.SecretKey == "" {
			return fmt.Errorf("BINANCE_SECRET_KEY is required in live mode")
		}
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required")
	}
	return nil
}
