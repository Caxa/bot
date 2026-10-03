package book

import "hedge/internal/exch"

// WalkAsks — средняя цена покупки на notionalUSD по asks.
func WalkAsks(levels []exch.Level, notionalUSD float64) (avg float64, filledUSD float64, ok bool) {
	return walk(levels, notionalUSD, true)
}

// WalkBids — средняя цена продажи на notionalUSD по bids.
func WalkBids(levels []exch.Level, notionalUSD float64) (avg float64, filledUSD float64, ok bool) {
	return walk(levels, notionalUSD, false)
}

func walk(levels []exch.Level, notionalUSD float64, buy bool) (avg, filled float64, ok bool) {
	if notionalUSD <= 0 || len(levels) == 0 {
		return 0, 0, false
	}
	var cost, qty float64
	need := notionalUSD
	for _, lv := range levels {
		if lv.Price <= 0 || lv.Qty <= 0 {
			continue
		}
		levelUSD := lv.Price * lv.Qty
		takeUSD := levelUSD
		if takeUSD > need {
			takeUSD = need
		}
		takeQty := takeUSD / lv.Price
		cost += takeUSD
		qty += takeQty
		need -= takeUSD
		if need <= 1e-9 {
			break
		}
		_ = buy
	}
	if qty <= 0 || cost < notionalUSD*0.95 {
		return 0, cost, false
	}
	return cost / qty, cost, true
}
