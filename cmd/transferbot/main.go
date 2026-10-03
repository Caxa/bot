package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hedge/internal/config"
	"hedge/internal/scan"
)

func main() {
	cfgPath := flag.String("config", "configs/transfer.yaml", "YAML-конфиг")
	liveTrade := flag.Bool("live-trade", false, "реальные ордера (пока отказано)")
	liveWithdraw := flag.Bool("live-withdraw", false, "реальный вывод (no-op)")
	minutes := flag.Int("minutes", 0, "бумажный режим: опрашивать ~каждые 45с в течение N минут вместо одного снимка")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *liveTrade {
		fmt.Fprintln(os.Stderr, "--live-trade отключён: пока только бумага")
		os.Exit(1)
	}

	if *minutes > 0 {
		if err := runPoll(cfg, *minutes, *liveWithdraw); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	res, err := scan.Run(cfg, *liveTrade, *liveWithdraw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("бумага: котировок %d, монет %d, instant-кандидатов %d\n", res.Quotes, res.MatchedCoins, len(res.Instant))
	fmt.Println("отчёт:", cfg.ReportFile)
	if len(res.Instant) > 0 {
		c := res.Instant[0]
		fmt.Printf("лучший снимок: %s %s -> %s net %.2f%%\n", c.Base, c.Buy.Exchange, c.Sell.Exchange, c.NetPct)
	}
}

// routeState — учёт одного маршрута монета+сеть+источник+приёмник через опросы.
type routeState struct {
	Base, Chain, Buy, Sell string
	FirstPoll              int
	LastPoll               int
	Held                   bool // net-плюс держался на каждом опросе после первого появления
	Confirmed              bool // хотя бы раз вывод и депозит были явно открыты
	Polls                  int  // сколько раз вообще был net-положительным (не обязательно подряд)
}

func routeKey(base, chain, buy, sell string) string {
	return base + "|" + chain + "|" + buy + "|" + sell
}

func explicitlyClosed(c scan.Candidate) bool {
	if c.Buy.WithdrawEnabled != nil && !*c.Buy.WithdrawEnabled {
		return true
	}
	if c.Sell.DepositEnabled != nil && !*c.Sell.DepositEnabled {
		return true
	}
	return false
}

func walletConfirmed(c scan.Candidate) bool {
	return c.Buy.WithdrawEnabled != nil && *c.Buy.WithdrawEnabled &&
		c.Sell.DepositEnabled != nil && *c.Sell.DepositEnabled
}

// tradable — плюс после комиссий (включая комиссию вывода, реальную или хотя
// бы оценённую по сети), стакан берёт размер, ввод/вывод не закрыты явно.
// Неизвестный статус кошелька не считается подтверждённым переводом. Если
// комиссию вывода вообще не с чем сверить (ни биржа, ни оценка по сети) —
// маршрут не считается сигналом: слишком велик риск, что Net на самом деле
// отрицательный.
func tradable(c scan.Candidate) bool {
	feeMeasured := c.FeeKnown || c.FeeEstimated
	return c.NetUSD > 0 && feeMeasured && !c.Thin && !explicitlyClosed(c)
}

func runPoll(cfg config.Config, minutes int, liveWithdraw bool) error {
	interval := 45 * time.Second
	deadline := time.Now().Add(time.Duration(minutes) * time.Minute)

	tracked := map[string]*routeState{}
	pollCount := 0
	var pollLog []string

	for {
		pollCount++
		res, err := scan.Run(cfg, false, liveWithdraw)
		if err != nil {
			return err
		}

		current := map[string]scan.Candidate{}
		for _, c := range res.Instant {
			if !tradable(c) {
				continue
			}
			key := routeKey(c.Base, c.Chain, c.Buy.Exchange, c.Sell.Exchange)
			// одна котировка на маршрут за опрос — берём лучшую по NetUSD
			if old, ok := current[key]; !ok || c.NetUSD > old.NetUSD {
				current[key] = c
			}
		}

		// маршруты, известные ДО этого опроса: если сейчас их нет среди положительных — held=false
		for key, st := range tracked {
			if _, ok := current[key]; !ok {
				st.Held = false
			}
		}

		for key, c := range current {
			st, ok := tracked[key]
			if !ok {
				st = &routeState{Base: c.Base, Chain: c.Chain, Buy: c.Buy.Exchange, Sell: c.Sell.Exchange, FirstPoll: pollCount, Held: true}
				tracked[key] = st
			}
			st.LastPoll = pollCount
			st.Polls++
			if walletConfirmed(c) {
				st.Confirmed = true
			}
		}

		pollLog = append(pollLog, fmt.Sprintf("опрос %d (%s): instant=%d, прошли фильтр=%d", pollCount, time.Now().UTC().Format(time.RFC3339), len(res.Instant), len(current)))
		fmt.Println(pollLog[len(pollLog)-1])

		if time.Now().Add(interval).After(deadline) {
			break
		}
		time.Sleep(interval)
	}

	var signals, held, confirmed int
	var rows []*routeState
	for _, st := range tracked {
		signals++
		if st.Held {
			held++
		}
		if st.Confirmed {
			confirmed++
		}
		rows = append(rows, st)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Held != rows[j].Held {
			return rows[i].Held
		}
		return rows[i].Polls > rows[j].Polls
	})

	if err := writePollReport(cfg, cfg.PollReportFile, minutes, pollCount, pollLog, rows, signals, held, confirmed); err != nil {
		return err
	}

	fmt.Printf("SIGNALS=%d\n", signals)
	fmt.Printf("HELD=%d\n", held)
	fmt.Printf("CONFIRMED=%d\n", confirmed)
	return nil
}

func writePollReport(cfg config.Config, path string, minutes, polls int, log []string, rows []*routeState, signals, held, confirmed int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Бумажный скан перевода: %d минут, опрос ~каждые 45с\n\n", minutes)
	fmt.Fprintf(&b, "Сформировано: %s. Опросов: %d. Только публичные рыночные данные, без ордеров и выводов.\n\n", time.Now().UTC().Format(time.RFC3339), polls)
	b.WriteString("Маршрут (монета+сеть+источник+приёмник) засчитан один раз на весь скан, не один раз за опрос. ")
	b.WriteString("Монета совпала по network+contract или native-белому списку. ")
	b.WriteString("SIGNALS требует плюс после комиссий, стакан на размер notional и отсутствие явно закрытого ввода/вывода. ")
	b.WriteString("CONFIRMED дополнительно требует, что вывод и депозит были явно открыты.\n\n")
	fmt.Fprintf(&b, "**SIGNALS=%d** — различных маршрутов с плюсом после комиссий, нормальным стаканом и без явно закрытого ввода/вывода.\n\n", signals)
	fmt.Fprintf(&b, "**HELD=%d** — из них маршрутов, которые оставались такими на каждом опросе после первого появления.\n\n", held)
	fmt.Fprintf(&b, "**CONFIRMED=%d** — из SIGNALS маршрутов, где вывод на источнике и депозит на приёмнике хотя бы раз были явно открыты. Неизвестный статус кошелька сюда не входит.\n\n", confirmed)
	b.WriteString("10 минут не доказывают ETA вывода 15–60 минут — это только то, что видно в самом окне скана.\n\n")

	b.WriteString("## Маршруты\n\n")
	b.WriteString("| Монета | Сеть | Купить | Продать | Первый опрос | Последний опрос | Опросов с плюсом | HELD |\n|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		heldStr := "нет"
		if r.Held {
			heldStr = "да"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %d | %d | %s |\n",
			r.Base, r.Chain, r.Buy, r.Sell, r.FirstPoll, r.LastPoll, r.Polls, heldStr)
	}
	if len(rows) == 0 {
		b.WriteString("| — | — | — | — | — | — | — | — |\n")
	}

	b.WriteString("\n## Журнал опросов\n\n")
	for _, l := range log {
		fmt.Fprintf(&b, "- %s\n", l)
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}
