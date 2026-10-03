package exch

import "time"

type binanceBookTicker struct {
	Symbol   string `json:"symbol"`
	BidPrice string `json:"bidPrice"`
	BidQty   string `json:"bidQty"`
	AskPrice string `json:"askPrice"`
	AskQty   string `json:"askQty"`
}

type binanceTicker24 struct {
	Symbol      string `json:"symbol"`
	QuoteVolume string `json:"quoteVolume"`
}

type Binance struct{}

func (Binance) Name() string { return "binance" }

func (Binance) Quotes() ([]Quote, error) {
	var books []binanceBookTicker
	if err := getJSON("https://api.binance.com/api/v3/ticker/bookTicker", &books); err != nil {
		return nil, err
	}
	var d24 []binanceTicker24
	vol := map[string]float64{}
	if err := getJSON("https://api.binance.com/api/v3/ticker/24hr", &d24); err == nil {
		for _, t := range d24 {
			vol[t.Symbol] = parseF(t.QuoteVolume)
		}
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 400)
	for _, t := range books {
		base, quote, ok := splitUSDT(t.Symbol)
		if !ok {
			continue
		}
		bid, ask := parseF(t.BidPrice), parseF(t.AskPrice)
		if bid <= 0 || ask <= 0 || bid >= ask {
			continue
		}
		out = append(out, Quote{
			Exchange:  "binance",
			Symbol:    t.Symbol,
			Base:      base,
			QuoteCcy:  quote,
			BidUSD:    bid,
			AskUSD:    ask,
			FxToUSD:   1,
			BidQty:    parseF(t.BidQty),
			AskQty:    parseF(t.AskQty),
			VolumeUSD: vol[t.Symbol],
			Native:    true,
			FetchedAt: now,
		})
	}
	return out, nil
}

func (Binance) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	}
	err := getJSON("https://api.binance.com/api/v3/depth?limit=50&symbol="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	return bookFromLevels(raw.Bids, raw.Asks), nil
}

func splitUSDT(sym string) (base, quote string, ok bool) {
	for _, q := range []string{"USDT", "USDC"} {
		if len(sym) > len(q) && sym[len(sym)-len(q):] == q {
			return sym[:len(sym)-len(q)], q, true
		}
	}
	return "", "", false
}

func bookFromLevels(bids, asks [][]string) Book {
	b := Book{}
	for _, x := range bids {
		if len(x) >= 2 {
			b.Bids = append(b.Bids, Level{Price: parseF(x[0]), Qty: parseF(x[1])})
		}
	}
	for _, x := range asks {
		if len(x) >= 2 {
			b.Asks = append(b.Asks, Level{Price: parseF(x[0]), Qty: parseF(x[1])})
		}
	}
	return b
}
