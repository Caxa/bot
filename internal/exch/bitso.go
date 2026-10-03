package exch

import (
	"strings"
	"time"
)

type Bitso struct{}

func (Bitso) Name() string { return "bitso" }

func (Bitso) Quotes() ([]Quote, error) {
	var raw struct {
		Payload []struct {
			Book   string `json:"book"`
			Bid    string `json:"bid"`
			Ask    string `json:"ask"`
			Volume string `json:"volume"`
			Vwap   string `json:"vwap"`
		} `json:"payload"`
	}
	if err := getJSON("https://api.bitso.com/v3/ticker", &raw); err != nil {
		return nil, err
	}
	usdMxn := 0.0
	for _, t := range raw.Payload {
		if t.Book == "usd_mxn" || t.Book == "usdt_mxn" {
			usdMxn = (parseF(t.Bid) + parseF(t.Ask)) / 2
		}
	}
	if usdMxn <= 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 40)
	for _, t := range raw.Payload {
		parts := strings.Split(t.Book, "_")
		if len(parts) != 2 || parts[1] != "mxn" {
			continue
		}
		base := strings.ToUpper(parts[0])
		if base == "USD" || base == "USDT" {
			continue
		}
		bid, ask := parseF(t.Bid)/usdMxn, parseF(t.Ask)/usdMxn
		if bid <= 0 || ask <= 0 {
			continue
		}
		out = append(out, Quote{
			Exchange:  "bitso",
			Symbol:    t.Book,
			Base:      base,
			QuoteCcy:  "MXN",
			BidUSD:    bid,
			AskUSD:    ask,
			FxToUSD:   1 / usdMxn,
			VolumeUSD: parseF(t.Volume) * bid,
			Native:    true,
			FetchedAt: now,
		})
	}
	return out, nil
}

func (Bitso) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Payload struct {
			Bids []struct {
				Price  string `json:"price"`
				Amount string `json:"amount"`
			} `json:"bids"`
			Asks []struct {
				Price  string `json:"price"`
				Amount string `json:"amount"`
			} `json:"asks"`
		} `json:"payload"`
	}
	err := getJSON("https://api.bitso.com/v3/order_book/?book="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	b := Book{}
	for _, x := range raw.Payload.Bids {
		b.Bids = append(b.Bids, Level{Price: parseF(x.Price), Qty: parseF(x.Amount)})
	}
	for _, x := range raw.Payload.Asks {
		b.Asks = append(b.Asks, Level{Price: parseF(x.Price), Qty: parseF(x.Amount)})
	}
	return b, nil
}
