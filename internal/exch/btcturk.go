package exch

import "time"

type BTCTurk struct{}

func (BTCTurk) Name() string { return "btcturk" }

func (BTCTurk) Quotes() ([]Quote, error) {
	var raw struct {
		Data []struct {
			Pair              string  `json:"pair"`
			Bid               float64 `json:"bid"`
			Ask               float64 `json:"ask"`
			Volume            float64 `json:"volume"`
			DenominatorSymbol string  `json:"denominatorSymbol"`
			NumeratorSymbol   string  `json:"numeratorSymbol"`
		} `json:"data"`
	}
	if err := getJSON("https://api.btcturk.com/api/v2/ticker", &raw); err != nil {
		return nil, err
	}
	usdtTRY := 0.0
	for _, t := range raw.Data {
		if t.NumeratorSymbol == "USDT" && t.DenominatorSymbol == "TRY" && t.Bid > 0 {
			usdtTRY = (t.Bid + t.Ask) / 2
		}
	}
	if usdtTRY <= 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 80)
	for _, t := range raw.Data {
		if t.DenominatorSymbol != "TRY" || t.NumeratorSymbol == "USDT" {
			continue
		}
		if t.Bid <= 0 || t.Ask <= 0 {
			continue
		}
		bidUSD := t.Bid / usdtTRY
		askUSD := t.Ask / usdtTRY
		if bidUSD <= 0 || askUSD <= 0 {
			continue
		}
		out = append(out, Quote{
			Exchange:  "btcturk",
			Symbol:    t.Pair,
			Base:      t.NumeratorSymbol,
			QuoteCcy:  "TRY",
			BidUSD:    bidUSD,
			AskUSD:    askUSD,
			FxToUSD:   1 / usdtTRY,
			VolumeUSD: t.Volume * bidUSD,
			Native:    true,
			FetchedAt: now,
		})
	}
	return out, nil
}

func (BTCTurk) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Data struct {
			Bids [][]any `json:"bids"`
			Asks [][]any `json:"asks"`
		} `json:"data"`
	}
	err := getJSON("https://api.btcturk.com/api/v2/orderbook?pairSymbol="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	return Book{Bids: anyLevels(raw.Data.Bids), Asks: anyLevels(raw.Data.Asks)}, nil
}

func anyLevels(rows [][]any) []Level {
	var out []Level
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		out = append(out, Level{Price: asF(r[0]), Qty: asF(r[1])})
	}
	return out
}

func asF(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		return parseF(x)
	default:
		return 0
	}
}
