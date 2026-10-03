package match

import (
	"strings"

	"hedge/internal/exch"
)

var natives = map[string]bool{
	"BTC": true, "ETH": true, "SOL": true, "XRP": true, "TRX": true,
	"DOGE": true, "LTC": true, "ADA": true, "DOT": true, "TON": true,
	"BNB": true, "ATOM": true, "NEAR": true, "AVAX": true, "SUI": true,
	"APT": true, "LINK": true, "UNI": true, "AAVE": true, "FIL": true,
}

func normChain(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch s {
	case "ERC20", "ETH", "ETHEREUM":
		return "ETH"
	case "TRC20", "TRX", "TRON":
		return "TRX"
	case "BEP20", "BSC", "BNB":
		return "BSC"
	case "SOL", "SOLANA":
		return "SOL"
	case "BITCOIN", "BTC":
		return "BTC"
	case "ARBITRUM", "ARB":
		return "ARB"
	case "POLYGON", "MATIC", "POL":
		return "POLYGON"
	case "TON", "TONCOIN":
		return "TON"
	}
	return s
}

// Key — одна монета. Контракт важнее тикера.
func Key(q exch.Quote) (string, bool) {
	chain := normChain(q.Chain)
	contract := strings.ToLower(strings.TrimSpace(q.Contract))
	if contract != "" && contract != "null" && contract != "0x" {
		if chain == "" {
			chain = "UNK"
		}
		return chain + "|" + contract, true
	}
	base := strings.ToUpper(q.Base)
	if natives[base] {
		if chain == "" {
			chain = base
			if base == "BNB" {
				chain = "BSC"
			}
		}
		return "native|" + base, true
	}
	return "", false
}

func ETAMinutes(chain string) int {
	switch normChain(chain) {
	case "BTC":
		return 60
	case "ETH", "ARB", "POLYGON":
		return 20
	case "SOL", "TRX", "BSC", "TON":
		return 15
	default:
		return 30
	}
}

func IsLeveraged(symbol string) bool {
	s := strings.ToUpper(symbol)
	return strings.Contains(s, "3L") || strings.Contains(s, "3S") ||
		strings.HasSuffix(s, "UP") && strings.Contains(s, "BULL")
}
