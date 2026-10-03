package scan

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hedge/internal/book"
	"hedge/internal/config"
	"hedge/internal/exch"
	"hedge/internal/match"
	"hedge/internal/pipeline"
)

type Candidate struct {
	Key            string
	Base           string
	Chain          string
	Contract       string
	Buy            exch.Quote
	Sell           exch.Quote
	GrossPct       float64
	NetUSD         float64
	NetPct         float64
	FeeUSD         float64
	FeeKnown       bool // комиссию вывода отдала сама биржа
	FeeEstimated   bool // комиссии не было в API, подставлена грубая оценка по сети
	ETAMin         int
	Thin           bool
	WalletNote     string
	Persistence    string
	PaperNextState pipeline.State
}

type Result struct {
	Started           time.Time
	ExchangeNotes     []string
	Quotes            int
	MatchedCoins      int
	Instant           []Candidate
	Anomalies         int
	RejectedClosed    int
	RejectedUnmatched int
}

func Run(cfg config.Config, liveTrade, liveWithdraw bool) (Result, error) {
	if liveWithdraw {
		fmt.Println(" --live-withdraw: no-op (вывод не вызывается в этой версии)")
	}
	if liveTrade {
		return Result{}, fmt.Errorf(" --live-trade отключён: пока только бумага")
	}

	names := uniq(append(append([]string{}, cfg.Sources...), cfg.Sinks...))
	quotes := map[string][]exch.Quote{}
	var notes []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range names {
		ad := exch.Lookup(name)
		if ad == nil {
			notes = append(notes, name+": неизвестная биржа")
			continue
		}
		wg.Add(1)
		go func(name string, ad exch.Adapter) {
			defer wg.Done()
			qs, err := ad.Quotes()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				notes = append(notes, name+": "+err.Error())
				return
			}
			quotes[name] = qs
			notes = append(notes, fmt.Sprintf("%s: %d котировок", name, len(qs)))
		}(name, ad)
	}
	wg.Wait()

	byKey := map[string][]exch.Quote{}
	anomalies := 0
	unmatched := 0
	now := time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(cfg.CSVFile), 0o755); err != nil {
		return Result{}, err
	}
	csvF, err := os.OpenFile(cfg.CSVFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Result{}, err
	}
	defer csvF.Close()
	st, _ := csvF.Stat()
	w := csv.NewWriter(csvF)
	if st.Size() == 0 {
		w.Write([]string{"time", "exchange", "base", "symbol", "chain", "contract", "bid_usd", "ask_usd", "volume_usd", "withdraw", "deposit"})
	}

	nQuotes := 0
	for _, qs := range quotes {
		for _, q := range qs {
			nQuotes++
			if match.IsLeveraged(q.Symbol) {
				anomalies++
				continue
			}
			key, ok := match.Key(q)
			if !ok {
				unmatched++
				continue
			}
			byKey[key] = append(byKey[key], q) // ниже оставим одну котировку на биржу (USDT)
			wEn, dEn := "", ""
			if q.WithdrawEnabled != nil {
				wEn = fmt.Sprintf("%v", *q.WithdrawEnabled)
			}
			if q.DepositEnabled != nil {
				dEn = fmt.Sprintf("%v", *q.DepositEnabled)
			}
			w.Write([]string{
				now.Format(time.RFC3339), q.Exchange, q.Base, q.Symbol, q.Chain, q.Contract,
				fmt.Sprintf("%.8f", q.BidUSD), fmt.Sprintf("%.8f", q.AskUSD),
				fmt.Sprintf("%.2f", q.VolumeUSD), wEn, dEn,
			})
		}
	}
	w.Flush()

	for key, qs := range byKey {
		byKey[key] = onePerExchange(qs)
	}

	srcSet := setOf(cfg.Sources)
	sinkSet := setOf(cfg.Sinks)
	var instant []Candidate
	closed := 0
	for key, qs := range byKey {
		if len(qs) < 2 {
			continue
		}
		var buys, sells []exch.Quote
		for _, q := range qs {
			if srcSet[q.Exchange] {
				buys = append(buys, q)
			}
			if sinkSet[q.Exchange] {
				sells = append(sells, q)
			}
		}
		for _, buy := range buys {
			for _, sell := range sells {
				if buy.Exchange == sell.Exchange {
					continue
				}
				if buy.AskUSD <= 0 || sell.BidUSD <= 0 {
					continue
				}
				if sell.DepositEnabled != nil && !*sell.DepositEnabled {
					closed++
					continue
				}
				if buy.WithdrawEnabled != nil && !*buy.WithdrawEnabled {
					closed++
					continue
				}
				feeBuy := cfg.Fee(buy.Exchange) / 100
				feeSell := cfg.Fee(sell.Exchange) / 100
				grossPct := (sell.BidUSD/buy.AskUSD - 1) * 100
				coins := cfg.NotionalUSD / buy.AskUSD
				proceeds := sell.BidUSD * coins * (1 - feeSell)
				cost := cfg.NotionalUSD * (1 + feeBuy)

				// Комиссию вывода знает не каждая биржа через публичный API
				// (например Gate её не отдаёт без ключа). Без ключа считаем
				// её $0 в буквальном смысле только если ни сама биржа, ни
				// оценка по сети ничего не дали — это НЕ значит, что вывод
				// бесплатный, это значит "не измерили".
				withdrawFeeUSD := buy.WithdrawFeeUSD
				feeEstimated := false
				if !buy.WithdrawFeeKnown {
					if est, ok := exch.ApproxWithdrawFeeUSD(firstNonEmpty(buy.Chain, sell.Chain)); ok {
						withdrawFeeUSD = est
						feeEstimated = true
					}
				}

				netUSD := proceeds - cost - withdrawFeeUSD
				netPct := netUSD / cfg.NotionalUSD * 100
				if netUSD <= 0 {
					continue
				}
				wallet := "unknown"
				if buy.WithdrawEnabled != nil && sell.DepositEnabled != nil {
					wallet = "open"
				}
				c := Candidate{
					Key:            key,
					Base:           buy.Base,
					Chain:          firstNonEmpty(buy.Chain, sell.Chain),
					Contract:       firstNonEmpty(buy.Contract, sell.Contract),
					Buy:            buy,
					Sell:           sell,
					GrossPct:       grossPct,
					NetUSD:         netUSD,
					NetPct:         netPct,
					FeeUSD:         withdrawFeeUSD,
					FeeKnown:       buy.WithdrawFeeKnown,
					FeeEstimated:   feeEstimated,
					ETAMin:         match.ETAMinutes(firstNonEmpty(buy.Chain, sell.Chain, buy.Base)),
					WalletNote:     wallet,
					Persistence:    "instant_unproven",
					PaperNextState: pipeline.Bought,
				}
				instant = append(instant, c)
			}
		}
	}
	sort.Slice(instant, func(i, j int) bool { return instant[i].NetPct > instant[j].NetPct })
	if len(instant) > cfg.MaxInstant {
		instant = instant[:cfg.MaxInstant]
	}

	// глубина стакана для всех instant-кандидатов (уже обрезаны до 40 выше)
	for i := range instant {
		c := &instant[i]
		buyAd, sellAd := exch.Lookup(c.Buy.Exchange), exch.Lookup(c.Sell.Exchange)
		if buyAd == nil || sellAd == nil {
			c.Thin = true
			continue
		}
		bb, err1 := buyAd.OrderBook(c.Buy.Symbol)
		sb, err2 := sellAd.OrderBook(c.Sell.Symbol)
		if err1 != nil || err2 != nil {
			c.Thin = true
			continue
		}
		scaleBook(&bb, fxOr1(c.Buy))
		scaleBook(&sb, fxOr1(c.Sell))
		_, _, okBuy := book.WalkAsks(bb.Asks, cfg.NotionalUSD)
		_, _, okSell := book.WalkBids(sb.Bids, cfg.NotionalUSD)
		c.Thin = !okBuy || !okSell
	}

	res := Result{
		Started:           now,
		ExchangeNotes:     notes,
		Quotes:            nQuotes,
		MatchedCoins:      len(byKey),
		Instant:           instant,
		Anomalies:         anomalies,
		RejectedClosed:    closed,
		RejectedUnmatched: unmatched,
	}
	if err := writeReport(cfg, res); err != nil {
		return res, err
	}
	_ = appendJSONL(cfg.SnapshotsFile, map[string]any{
		"time": now, "quotes": nQuotes, "matched": len(byKey), "instant": len(instant),
	})
	return res, nil
}

func scaleBook(b *exch.Book, fx float64) {
	if fx == 0 || fx == 1 {
		return
	}
	for i := range b.Bids {
		b.Bids[i].Price *= fx
	}
	for i := range b.Asks {
		b.Asks[i].Price *= fx
	}
}

func fxOr1(q exch.Quote) float64 {
	if q.FxToUSD == 0 {
		return 1
	}
	return q.FxToUSD
}

func writeReport(cfg config.Config, r Result) error {
	if err := os.MkdirAll(filepath.Dir(cfg.ReportFile), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Бумажный скан перевода между биржами\n\n")
	fmt.Fprintf(&b, "Сформировано: %s\n\n", r.Started.Format(time.RFC3339))
	fmt.Fprintf(&b, "Реальных ордеров и выводов не было. Сверка монет: сеть+контракт, иначе native-белый список.\n\n")
	fmt.Fprintf(&b, "Котировок: **%d**. Монет с ключом: **%d**. Instant-кандидатов (истории ещё нет): **%d**.\n\n", r.Quotes, r.MatchedCoins, len(r.Instant))
	fmt.Fprintf(&b, "Отброшено: коллизии/плечи %d, без ключа идентичности %d, закрытый ввод/вывод %d.\n\n", r.Anomalies, r.RejectedUnmatched, r.RejectedClosed)
	b.WriteString("## Биржи\n\n")
	for _, n := range r.ExchangeNotes {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	b.WriteString("\n## Instant, не доказана живучесть (один снимок)\n\n")
	b.WriteString("Это не «сделка состоялась». Это факт: в эту секунду bid на приёмнике выше ask на источнике после торговых комиссий. Без истории на 2×ETA перевод ещё не доказан.\n\n")
	b.WriteString("Комиссия вывода: «биржа» — цифру отдал сам API биржи (реальная). «оценка» — биржа комиссию не публикует (например Gate её не отдаёт без ключа), подставлена грубая оценка по сети из `internal/exch/feeguess.go` — проверяйте на сайте биржи перед реальным выводом. «$0?» — ни биржа, ни оценка ничего не дали: Net в таблице может быть завышен.\n\n")
	fmt.Fprintf(&b, "| Монета | Купить | Продать | Gross %% | Комиссия вывода $ | Источник комиссии | Net $ на %.0f | Net %% | ETA мин | Стакан | Кошелёк |\n|---|---|---|---|---|---|---|---|---|---|---|\n", cfg.NotionalUSD)
	for _, c := range r.Instant {
		thin := "ок"
		if c.Thin {
			thin = "тонкий"
		}
		feeSrc := "биржа"
		switch {
		case c.FeeKnown:
			feeSrc = "биржа"
		case c.FeeEstimated:
			feeSrc = "оценка"
		default:
			feeSrc = "$0?"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %.3f | %.2f | %s | %.2f | %.3f | %d | %s | %s |\n",
			c.Base, c.Buy.Exchange, c.Sell.Exchange, c.GrossPct, c.FeeUSD, feeSrc, c.NetUSD, c.NetPct, c.ETAMin, thin, c.WalletNote)
	}
	b.WriteString("\nПовторяйте `go run ./cmd/transferbot` каждые несколько минут — в CSV копится история для фильтра живучести.\n")
	return os.WriteFile(cfg.ReportFile, []byte(b.String()), 0o644)
}

func appendJSONL(path string, v any) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(v)
}

func onePerExchange(qs []exch.Quote) []exch.Quote {
	best := map[string]exch.Quote{}
	for _, q := range qs {
		old, ok := best[q.Exchange]
		if !ok || (q.QuoteCcy == "USDT" && old.QuoteCcy != "USDT") || q.VolumeUSD > old.VolumeUSD && !(old.QuoteCcy == "USDT" && q.QuoteCcy != "USDT") {
			best[q.Exchange] = q
		}
	}
	out := make([]exch.Quote, 0, len(best))
	for _, q := range best {
		out = append(out, q)
	}
	return out
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func setOf(in []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range in {
		m[s] = true
	}
	return m
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
