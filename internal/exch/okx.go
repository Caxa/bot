package exch

import (
	"strings"
	"time"
)

type OKX struct{}

func (OKX) Name() string { return "okx" }

func (OKX) Quotes() ([]Quote, error) {
	var raw struct {
		Data []struct {
			InstID    string `json:"instId"`
			BidPx     string `json:"bidPx"`
			BidSz     string `json:"bidSz"`
			AskPx     string `json:"askPx"`
			AskSz     string `json:"askSz"`
			VolCcy24h string `json:"volCcy24h"`
		} `json:"data"`
	}
	if err := getJSON("https://www.okx.com/api/v5/market/tickers?instType=SPOT", &raw); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 300)
	for _, t := range raw.Data {
		parts := strings.Split(t.InstID, "-")
		if len(parts) != 2 {
			continue
		}
		base, quote := parts[0], parts[1]
		if quote != "USDT" && quote != "USDC" {
			continue
		}
		bid, ask := parseF(t.BidPx), parseF(t.AskPx)
		if bid <= 0 || ask <= 0 || bid >= ask {
			continue
		}
		out = append(out, Quote{
			Exchange:  "okx",
			Symbol:    t.InstID,
			Base:      base,
			QuoteCcy:  quote,
			BidUSD:    bid,
			AskUSD:    ask,
			FxToUSD:   1,
			BidQty:    parseF(t.BidSz),
			AskQty:    parseF(t.AskSz),
			VolumeUSD: parseF(t.VolCcy24h),
			Native:    true,
			FetchedAt: now,
		})
	}
	return out, nil
}

func (OKX) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Data []struct {
			Bids [][]string `json:"bids"`
			Asks [][]string `json:"asks"`
		} `json:"data"`
	}
	err := getJSON("https://www.okx.com/api/v5/market/books?sz=50&instId="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	if len(raw.Data) == 0 {
		return Book{}, nil
	}
	return bookFromLevels(raw.Data[0].Bids, raw.Data[0].Asks), nil
}
