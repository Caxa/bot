package exch

import (
	"strings"
	"time"
)

type Gate struct{}

func (Gate) Name() string { return "gate" }

func (Gate) Quotes() ([]Quote, error) {
	meta := map[string]gateMeta{}
	if m, err := gateCurrencies(); err == nil {
		meta = m
	}
	var raw []struct {
		Pair        string `json:"currency_pair"`
		HighestBid  string `json:"highest_bid"`
		LowestAsk   string `json:"lowest_ask"`
		QuoteVolume string `json:"quote_volume"`
	}
	if err := getJSON("https://api.gateio.ws/api/v4/spot/tickers", &raw); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 400)
	for _, t := range raw {
		parts := strings.Split(t.Pair, "_")
		if len(parts) != 2 {
			continue
		}
		base, quote := parts[0], parts[1]
		if quote != "USDT" && quote != "USDC" {
			continue
		}
		bid, ask := parseF(t.HighestBid), parseF(t.LowestAsk)
		if bid <= 0 || ask <= 0 || bid >= ask {
			continue
		}
		q := Quote{
			Exchange:  "gate",
			Symbol:    t.Pair,
			Base:      base,
			QuoteCcy:  quote,
			BidUSD:    bid,
			AskUSD:    ask,
			FxToUSD:   1,
			VolumeUSD: parseF(t.QuoteVolume),
			FetchedAt: now,
		}
		if g, ok := meta[strings.ToUpper(base)]; ok {
			q.Chain = g.Chain
			q.Contract = g.Contract
			q.WithdrawEnabled = boolPtr(!g.WithdrawOff)
			q.DepositEnabled = boolPtr(!g.DepositOff)
			q.Native = g.Contract == ""
		}
		out = append(out, q)
	}
	return out, nil
}

func (Gate) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	}
	err := getJSON("https://api.gateio.ws/api/v4/spot/order_book?limit=50&currency_pair="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	return bookFromLevels(raw.Bids, raw.Asks), nil
}

type gateMeta struct {
	Chain, Contract         string
	WithdrawOff, DepositOff bool
}

func gateCurrencies() (map[string]gateMeta, error) {
	var raw []struct {
		Currency         string `json:"currency"`
		WithdrawDisabled bool   `json:"withdraw_disabled"`
		DepositDisabled  bool   `json:"deposit_disabled"`
		Chain            string `json:"chain"`
		Chains           []struct {
			Name             string `json:"name"`
			Addr             string `json:"addr"`
			WithdrawDisabled bool   `json:"withdraw_disabled"`
			DepositDisabled  bool   `json:"deposit_disabled"`
		} `json:"chains"`
	}
	if err := getJSON("https://api.gateio.ws/api/v4/spot/currencies", &raw); err != nil {
		return nil, err
	}
	out := map[string]gateMeta{}
	for _, c := range raw {
		m := gateMeta{
			Chain:       c.Chain,
			WithdrawOff: c.WithdrawDisabled,
			DepositOff:  c.DepositDisabled,
		}
		if len(c.Chains) > 0 {
			ch := c.Chains[0]
			for _, x := range c.Chains {
				n := strings.ToUpper(x.Name)
				if n == "SOL" || n == "TRX" || n == "TRC20" || n == "BSC" || n == "BEP20" || n == "TON" {
					ch = x
					break
				}
			}
			m.Chain = ch.Name
			m.Contract = ch.Addr
			m.WithdrawOff = ch.WithdrawDisabled
			m.DepositOff = ch.DepositDisabled
		}
		out[strings.ToUpper(c.Currency)] = m
	}
	return out, nil
}
