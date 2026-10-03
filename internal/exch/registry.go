package exch

func Lookup(name string) Adapter {
	switch name {
	case "binance":
		return Binance{}
	case "bybit":
		return Bybit{}
	case "okx":
		return OKX{}
	case "bitget":
		return Bitget{}
	case "gate":
		return Gate{}
	case "upbit":
		return Upbit{}
	case "btcturk":
		return BTCTurk{}
	case "bitso":
		return Bitso{}
	case "indodax":
		return Indodax{}
	default:
		return nil
	}
}
