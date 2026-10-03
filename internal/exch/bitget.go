package exch

import (
	"strings"
	"time"
)

type Bitget struct{}

func (Bitget) Name() string { return "bitget" }

func (Bitget) Quotes() ([]Quote, error) {
	chains := map[string]coinChain{}
	if cs, err := bitgetCoins(); err == nil {
		chains = cs
	}
	var raw struct {
		Data []struct {
			Symbol      string `json:"symbol"`
			BidPr       string `json:"bidPr"`
			BidSz       string `json:"bidSz"`
			AskPr       string `json:"askPr"`
			AskSz       string `json:"askSz"`
			QuoteVolume string `json:"quoteVolume"`
			BaseVolume  string `json:"baseVolume"`
		} `json:"data"`
	}
	if err := getJSON("https://api.bitget.com/api/v2/spot/market/tickers", &raw); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]Quote, 0, 400)
	for _, t := range raw.Data {
		base, quote, ok := splitUSDT(t.Symbol)
		if !ok {
			continue
		}
		bid, ask := parseF(t.BidPr), parseF(t.AskPr)
		if bid <= 0 || ask <= 0 || bid >= ask {
			continue
		}
		q := Quote{
			Exchange:  "bitget",
			Symbol:    t.Symbol,
			Base:      base,
			QuoteCcy:  quote,
			BidUSD:    bid,
			AskUSD:    ask,
			FxToUSD:   1,
			BidQty:    parseF(t.BidSz),
			AskQty:    parseF(t.AskSz),
			VolumeUSD: parseF(t.QuoteVolume),
			FetchedAt: now,
		}
		if ch, ok := chains[strings.ToUpper(base)]; ok {
			q.Chain = ch.Chain
			q.Contract = ch.Contract
			q.WithdrawEnabled = boolPtr(ch.Withdraw)
			q.DepositEnabled = boolPtr(ch.Deposit)
			q.WithdrawFeeUSD = ch.Fee * bid
			q.WithdrawFeeKnown = true // bitget/public/coins отдаёт withdrawFee в токенах — это реальная цифра
			q.Native = ch.Contract == ""
		}
		out = append(out, q)
	}
	return out, nil
}

func (Bitget) OrderBook(symbol string) (Book, error) {
	var raw struct {
		Data struct {
			Bids [][]string `json:"bids"`
			Asks [][]string `json:"asks"`
		} `json:"data"`
	}
	err := getJSON("https://api.bitget.com/api/v2/spot/market/orderbook?limit=50&symbol="+symbol, &raw)
	if err != nil {
		return Book{}, err
	}
	return bookFromLevels(raw.Data.Bids, raw.Data.Asks), nil
}

type coinChain struct {
	Chain, Contract   string
	Withdraw, Deposit bool
	Fee               float64
}

func bitgetCoins() (map[string]coinChain, error) {
	var raw struct {
		Data []struct {
			Coin   string `json:"coin"`
			Chains []struct {
				Chain           string `json:"chain"`
				Withdrawable    string `json:"withdrawable"`
				Rechargeable    string `json:"rechargeable"`
				WithdrawFee     string `json:"withdrawFee"`
				ContractAddress any    `json:"contractAddress"`
			} `json:"chains"`
		} `json:"data"`
	}
	if err := getJSON("https://api.bitget.com/api/v2/spot/public/coins", &raw); err != nil {
		return nil, err
	}
	out := map[string]coinChain{}
	for _, c := range raw.Data {
		best, ok := pickChain(c.Chains)
		if !ok {
			continue
		}
		contract := ""
		switch v := best.ContractAddress.(type) {
		case string:
			contract = v
		}
		out[strings.ToUpper(c.Coin)] = coinChain{
			Chain:    best.Chain,
			Contract: contract,
			Withdraw: best.Withdrawable == "true",
			Deposit:  best.Rechargeable == "true",
			Fee:      parseF(best.WithdrawFee),
		}
	}
	return out, nil
}

func pickChain(chains []struct {
	Chain           string `json:"chain"`
	Withdrawable    string `json:"withdrawable"`
	Rechargeable    string `json:"rechargeable"`
	WithdrawFee     string `json:"withdrawFee"`
	ContractAddress any    `json:"contractAddress"`
}) (struct {
	Chain           string `json:"chain"`
	Withdrawable    string `json:"withdrawable"`
	Rechargeable    string `json:"rechargeable"`
	WithdrawFee     string `json:"withdrawFee"`
	ContractAddress any    `json:"contractAddress"`
}, bool) {
	if len(chains) == 0 {
		return struct {
			Chain           string `json:"chain"`
			Withdrawable    string `json:"withdrawable"`
			Rechargeable    string `json:"rechargeable"`
			WithdrawFee     string `json:"withdrawFee"`
			ContractAddress any    `json:"contractAddress"`
		}{}, false
	}
	pref := []string{"SOL", "TRC20", "TRX", "BEP20", "BSC", "TON", "ERC20", "ETH", "BTC"}
	for _, p := range pref {
		for _, ch := range chains {
			if strings.EqualFold(ch.Chain, p) {
				return ch, true
			}
		}
	}
	return chains[0], true
}
