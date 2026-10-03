package exch

import (
	"strings"
	"time"
)

type Indodax struct{}

func (Indodax) Name() string { return "indodax" }

func (Indodax) Quotes() ([]Quote, error) {
	var raw struct {
		Tickers map[string]struct {
			Last   string `json:"last"`
			Buy    string `json:"buy"`
			Sell   string `json:"sell"`
			VolIDR string `json:"vol_idr"`
		} `json:"tickers"`
	}
	if err := getJSON("https://indodax.com/api/ticker_all", &raw); err != nil {
		return nil, err
	}
	usdtIDR := 0.0
	if t, ok := raw.Tickers["usdt_idr"]; ok {
		usdtIDR = (parseF(t.Buy) + parseF(t.Sell)) / 2
		if usdtIDR <= 0 {
			usdtIDR = parseF(t.Last)
		}
	}
	if usdtIDR <= 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 80)
	for pair, t := range raw.Tickers {
		if !strings.HasSuffix(pair, "_idr") {
			continue
		}
		base := strings.ToUpper(strings.TrimSuffix(pair, "_idr"))
		if base == "USDT" {
			continue
		}
		bid, ask := parseF(t.Buy), parseF(t.Sell)
		if bid <= 0 {
			bid = parseF(t.Last)
		}
		if ask <= 0 {
			ask = parseF(t.Last)
		}
		if bid <= 0 || ask <= 0 {
			continue
		}
		out = append(out, Quote{
			Exchange:  "indodax",
			Symbol:    pair,
			Base:      base,
			QuoteCcy:  "IDR",
			BidUSD:    bid / usdtIDR,
			AskUSD:    ask / usdtIDR,
			FxToUSD:   1 / usdtIDR,
			VolumeUSD: parseF(t.VolIDR) / usdtIDR,
			Native:    true,
			FetchedAt: now,
		})
	}
	return out, nil
}

func (Indodax) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Bids [][]any `json:"bids"`
		Asks [][]any `json:"asks"`
	}
	err := getJSON("https://indodax.com/api/"+symbol+"/depth", &raw)
	if err != nil {
		return Book{}, err
	}
	return Book{Bids: anyLevels(raw.Bids), Asks: anyLevels(raw.Asks)}, nil
}
