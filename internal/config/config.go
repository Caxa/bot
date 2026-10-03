package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	NotionalUSD    float64            `yaml:"notional_usd"`
	PersistPolls   int                `yaml:"persist_polls"`
	MaxInstant     int                `yaml:"max_instant_candidates"`
	Sources        []string           `yaml:"sources"`
	Sinks          []string           `yaml:"sinks"`
	TakerFeePct    map[string]float64 `yaml:"taker_fee_pct"`
	SnapshotsFile  string             `yaml:"snapshots_file"`
	CSVFile        string             `yaml:"csv_file"`
	ReportFile     string             `yaml:"report_file"`
	PollReportFile string             `yaml:"poll_report_file"`
	AnomaliesFile  string             `yaml:"anomalies_file"`

	// Риск-менеджмент для живой (не бумажной) торговли — см. cmd/tradebot.
	MaxNotionalPerTradeUSD float64 `yaml:"max_notional_per_trade_usd"`
	MaxConcurrentTrades    int     `yaml:"max_concurrent_trades"`
	CooldownMinutes        int     `yaml:"cooldown_minutes"`
	SlippageBufferPct      float64 `yaml:"slippage_buffer_pct"`
	RequireFeeKnown        bool    `yaml:"require_fee_known"`
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if c.NotionalUSD == 0 {
		c.NotionalUSD = 100
	}
	if c.PersistPolls <= 0 {
		c.PersistPolls = 2
	}
	if c.MaxInstant <= 0 {
		c.MaxInstant = 40
	}
	if c.PollReportFile == "" {
		c.PollReportFile = "reports/transfer_scan_10min.md"
	}
	if c.MaxNotionalPerTradeUSD <= 0 {
		c.MaxNotionalPerTradeUSD = c.NotionalUSD
	}
	if c.MaxConcurrentTrades <= 0 {
		c.MaxConcurrentTrades = 1
	}
	if c.CooldownMinutes <= 0 {
		c.CooldownMinutes = 20
	}
	if c.SlippageBufferPct <= 0 {
		c.SlippageBufferPct = 0.15
	}
	return c, nil
}

func (c Config) Fee(exchange string) float64 {
	if c.TakerFeePct == nil {
		return 0.10
	}
	if v, ok := c.TakerFeePct[exchange]; ok {
		return v
	}
	return 0.10
}
