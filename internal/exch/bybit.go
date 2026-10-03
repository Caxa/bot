package exch

import "time"

type Bybit struct{}

func (Bybit) Name() string { return "bybit" }

func (Bybit) Quotes() ([]Quote, error) {
	var raw struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol      string `json:"symbol"`
				Bid1Price   string `json:"bid1Price"`
				Bid1Size    string `json:"bid1Size"`
				Ask1Price   string `json:"ask1Price"`
				Ask1Size    string `json:"ask1Size"`
				Turnover24h string `json:"turnover24h"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := getJSON("https://api.bybit.com/v5/market/tickers?category=spot", &raw); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 300)
	for _, t := range raw.Result.List {
		base, quote, ok := splitUSDT(t.Symbol)
		if !ok {
			continue
		}
		bid, ask := parseF(t.Bid1Price), parseF(t.Ask1Price)
		if bid <= 0 || ask <= 0 || bid >= ask {
			continue
		}
		out = append(out, Quote{
			Exchange:  "bybit",
			Symbol:    t.Symbol,
			Base:      base,
			QuoteCcy:  quote,
			BidUSD:    bid,
			AskUSD:    ask,
			FxToUSD:   1,
			BidQty:    parseF(t.Bid1Size),
			AskQty:    parseF(t.Ask1Size),
			VolumeUSD: parseF(t.Turnover24h),
			Native:    true,
			FetchedAt: now,
		})
	}
	return out, nil
}

func (Bybit) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Result struct {
			B [][]string `json:"b"`
			A [][]string `json:"a"`
		} `json:"result"`
	}
	err := getJSON("https://api.bybit.com/v5/market/orderbook?category=spot&limit=50&symbol="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	return bookFromLevels(raw.Result.B, raw.Result.A), nil
}
