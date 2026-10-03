// Command tlog опрашивает gate и bitget до заданного времени по МСК и пишет
// в текстовый файл каждую находку: где дешевле купить, где дороже продать,
// по какой цене, во сколько по МСК и сколько процентов вышло бы на сделке в
// $100 после торговых комиссий и комиссии вывода — только если перевод между
// биржами не заблокирован явно на обеих сторонах.
//
// Бумажный режим: ордеров и выводов нет, только публичные котировки.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hedge/internal/config"
	"hedge/internal/scan"
)

func main() {
	cfgPath := flag.String("config", "configs/transfer_t.yaml", "YAML-конфиг")
	outPath := flag.String("out", "t.txt", "куда дописывать находки")
	until := flag.String("until", "07:00", "время МСК (ЧЧ:ММ), до которого работать")
	interval := flag.Duration("interval", 45*time.Second, "пауза между опросами")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		fmt.Fprintln(os.Stderr, "часовой пояс МСК:", err)
		os.Exit(1)
	}

	deadline, err := nextMSKTime(*until, msk)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil && filepath.Dir(*outPath) != "." {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f, err := os.OpenFile(*outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	fmt.Fprintf(f, "=== старт %s, до %s МСК, пара gate<->bitget, нотионал $%.0f, бумага (без ордеров) ===\n",
		time.Now().In(msk).Format("02.01.2006 15:04:05"), deadline.Format("15:04"), cfg.NotionalUSD)
	fmt.Printf("tlog: пишу в %s, работаю до %s МСК\n", *outPath, deadline.Format("2006-01-02 15:04:05 MST"))

	poll := 0
	found := 0
	for {
		poll++
		res, err := scan.Run(cfg, false, false)
		if err != nil {
			line := fmt.Sprintf("%s | ошибка опроса: %v\n", time.Now().In(msk).Format("02.01 15:04:05"), err)
			fmt.Fprint(f, line)
			fmt.Print(line)
		} else {
			for _, c := range res.Instant {
				if !tradable(c) {
					continue
				}
				found++
				line := formatFind(c, msk)
				fmt.Fprint(f, line)
				fmt.Print(line)
			}
		}

		now := time.Now()
		if !now.Before(deadline) {
			break
		}
		sleepFor := *interval
		if now.Add(sleepFor).After(deadline) {
			sleepFor = deadline.Sub(now)
		}
		if sleepFor <= 0 {
			break
		}
		time.Sleep(sleepFor)
	}

	summary := fmt.Sprintf("=== стоп %s МСК, опросов: %d, находок: %d ===\n",
		time.Now().In(msk).Format("02.01.2006 15:04:05"), poll, found)
	fmt.Fprint(f, summary)
	fmt.Print(summary)
}

// tradable — плюс после торговых комиссий и комиссии вывода (реальной или
// оценённой по сети), стакан берёт $100, и перевод НЕ заблокирован: вывод на
// источнике и депозит на приёмнике явно открыты (не "неизвестно").
func tradable(c scan.Candidate) bool {
	feeMeasured := c.FeeKnown || c.FeeEstimated
	walletOpen := c.Buy.WithdrawEnabled != nil && *c.Buy.WithdrawEnabled &&
		c.Sell.DepositEnabled != nil && *c.Sell.DepositEnabled
	return c.NetUSD > 0 && feeMeasured && !c.Thin && walletOpen
}

func formatFind(c scan.Candidate, loc *time.Location) string {
	feeSrc := "оценка сети"
	if c.FeeKnown {
		feeSrc = "биржа"
	}
	return fmt.Sprintf(
		"%s МСК | %s | купить %s по %s | продать %s по %s | +%.3f%% нетто на $100 (торг.комиссии+вывод %.2f$ [%s] учтены) | перевод: открыт\n",
		time.Now().In(loc).Format("02.01 15:04:05"),
		c.Base,
		c.Buy.Exchange, fmtPrice(c.Buy.AskUSD),
		c.Sell.Exchange, fmtPrice(c.Sell.BidUSD),
		c.NetPct, c.FeeUSD, feeSrc,
	)
}

func fmtPrice(p float64) string {
	switch {
	case p >= 100:
		return fmt.Sprintf("%.2f", p)
	case p >= 1:
		return fmt.Sprintf("%.4f", p)
	default:
		return fmt.Sprintf("%.8f", p)
	}
}

// nextMSKTime разбирает "ЧЧ:ММ" и возвращает ближайший будущий момент этого
// времени по МСК: сегодня, если ещё не прошло, иначе завтра.
func nextMSKTime(hhmm string, loc *time.Location) (time.Time, error) {
	var h, m int
	if _, err := fmt.Sscanf(hhmm, "%d:%d", &h, &m); err != nil {
		return time.Time{}, fmt.Errorf("--until: ожидался формат ЧЧ:ММ, получено %q", hhmm)
	}
	now := time.Now().In(loc)
	d := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
	if !d.After(now) {
		d = d.Add(24 * time.Hour)
	}
	return d, nil
}
