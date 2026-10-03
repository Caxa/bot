package exch

import (
	"strings"
	"time"
)

type Upbit struct{}

func (Upbit) Name() string { return "upbit" }

func (Upbit) Quotes() ([]Quote, error) {
	var markets []struct {
		Market string `json:"market"`
	}
	if err := getJSON("https://api.upbit.com/v1/market/all", &markets); err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range markets {
		if strings.HasPrefix(m.Market, "KRW-") {
			ids = append(ids, m.Market)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// Upbit принимает список через запятую; режем пачками.
	now := time.Now().UTC()
	var all []upbitTicker
	for i := 0; i < len(ids); i += 80 {
		j := i + 80
		if j > len(ids) {
			j = len(ids)
		}
		var batch []upbitTicker
		url := "https://api.upbit.com/v1/ticker?markets=" + strings.Join(ids[i:j], ",")
		if err := getJSON(url, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
	}
	krwUSDT := 0.0
	for _, t := range all {
		if t.Market == "KRW-USDT" && t.TradePrice > 0 {
			krwUSDT = t.TradePrice
		}
	}
	if krwUSDT <= 0 {
		// запасной курс: KRW-BTC / глобальный BTC не тянем здесь — без USDT котировки нет
		return nil, nil
	}
	out := make([]Quote, 0, len(all))
	for _, t := range all {
		if !strings.HasPrefix(t.Market, "KRW-") {
			continue
		}
		base := strings.TrimPrefix(t.Market, "KRW-")
		if base == "USDT" {
			continue
		}
		bidKRW, askKRW := t.Bid, t.Ask
		if bidKRW <= 0 {
			bidKRW = t.TradePrice
		}
		if askKRW <= 0 {
			askKRW = t.TradePrice
		}
		if bidKRW <= 0 || askKRW <= 0 {
			continue
		}
		bidUSD := bidKRW / krwUSDT
		askUSD := askKRW / krwUSDT
		if bidUSD <= 0 || askUSD <= 0 {
			continue
		}
		if bidUSD > askUSD {
			askUSD = bidUSD * 1.001
		}
		out = append(out, Quote{
			Exchange:  "upbit",
			Symbol:    t.Market,
			Base:      base,
			QuoteCcy:  "KRW",
			BidUSD:    bidUSD,
			AskUSD:    askUSD,
			FxToUSD:   1 / krwUSDT,
			VolumeUSD: t.AccTradePrice24h / krwUSDT,
			Native:    true,
			FetchedAt: now,
		})
	}
	return out, nil
}

type upbitTicker struct {
	Market           string  `json:"market"`
	TradePrice       float64 `json:"trade_price"`
	AccTradePrice24h float64 `json:"acc_trade_price_24h"`
	Bid              float64 `json:"highest_bid"` // may be missing
	Ask              float64 `json:"lowest_ask"`
}

func (Upbit) OrderBook(symbol string) (Book, error) {
	var raw []struct {
		OrderbookUnits []struct {
			AskPrice float64 `json:"ask_price"`
			BidPrice float64 `json:"bid_price"`
			AskSize  float64 `json:"ask_size"`
			BidSize  float64 `json:"bid_size"`
		} `json:"orderbook_units"`
	}
	err := getJSON("https://api.upbit.com/v1/orderbook?markets="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	if len(raw) == 0 {
		return Book{}, nil
	}
	// Книга в KRW — walk идёт в USD в scan через отдельный FX. Здесь отдаём KRW-цены.
	b := Book{}
	for _, u := range raw[0].OrderbookUnits {
		b.Bids = append(b.Bids, Level{Price: u.BidPrice, Qty: u.BidSize})
		b.Asks = append(b.Asks, Level{Price: u.AskPrice, Qty: u.AskSize})
	}
	return b, nil
}
