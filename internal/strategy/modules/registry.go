package modules

// Registry holds all available scoring modules.
var registry = map[string]func() ScoringModule{}

// Register adds a module factory to the registry.
func Register(name string, factory func() ScoringModule) {
	registry[name] = factory
}

// Create instantiates a module by name and initializes it with params.
func Create(name string, params map[string]interface{}) (ScoringModule, bool) {
	factory, ok := registry[name]
	if !ok {
		return nil, false
	}
	m := factory()
	m.Init(params)
	return m, true
}

// AllMeta returns metadata for all registered modules.
func AllMeta() []ModuleMeta {
	result := make([]ModuleMeta, 0, len(registry))
	for _, factory := range registry {
		m := factory()
		result = append(result, Meta(m))
	}
	return result
}

// Available returns all registered module names.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

func init() {
	Register("ema_cross", func() ScoringModule { return &EMACrossModule{} })
	Register("macd", func() ScoringModule { return &MACDModule{} })
	Register("sma_trend", func() ScoringModule { return &SMATrendModule{} })
	Register("rsi", func() ScoringModule { return &RSIModule{} })
	Register("kdj", func() ScoringModule { return &KDJModule{} })
	Register("bb_position", func() ScoringModule { return &BBPositionModule{} })
	Register("mfi", func() ScoringModule { return &MFIModule{} })
	Register("cmf", func() ScoringModule { return &CMFModule{} })
	Register("volume_ratio", func() ScoringModule { return &VolumeRatioModule{} })
	Register("vroc", func() ScoringModule { return &VROCModule{} })
	Register("force_index", func() ScoringModule { return &ForceIndexModule{} })
	Register("obv_trend", func() ScoringModule { return &OBVTrendModule{} })
}
