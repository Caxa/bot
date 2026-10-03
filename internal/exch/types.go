package exch

import "time"

// Quote — лучшие bid/ask одной монеты на одной бирже, уже в долларах.
type Quote struct {
	Exchange         string
	Symbol           string // как на бирже: BTCUSDT, KRW-BTC
	Base             string
	QuoteCcy         string
	BidUSD           float64
	AskUSD           float64
	FxToUSD          float64 // 1 для USDT; KRW/TRY/MXN/IDR → доллар
	BidQty           float64
	AskQty           float64
	VolumeUSD        float64
	Chain            string
	Contract         string
	WithdrawEnabled  *bool // nil = неизвестно без ключа
	DepositEnabled   *bool
	WithdrawFeeUSD   float64
	WithdrawFeeKnown bool // true только если биржа реально отдала комиссию вывода в публичном API
	Native           bool
	FetchedAt        time.Time
}

type Level struct {
	Price float64
	Qty   float64
}

type Book struct {
	Bids []Level // лучший первым
	Asks []Level
}

type Adapter interface {
	Name() string
	Quotes() ([]Quote, error)
	OrderBook(symbol string) (Book, error)
}
