// Command tradebot — полуавтоматический арбитражный бот между Gate и Bitget.
//
// Находит спред, фильтрует по риску (см. README/конфиг), печатает карточку
// сделки и ждёт вашего подтверждения (y/n) перед КАЖДЫМ необратимым шагом:
// покупка → вывод → продажа. Без --execute ничего не вызывает по-настоящему —
// только показывает, что бы сделал (бумага).
//
// Ключи берутся из .env (GATE_API_KEY/SECRET, BITGET_API_KEY/SECRET/PASSPHRASE).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"hedge/internal/config"
	"hedge/internal/exch"
	"hedge/internal/scan"
)

func loadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	return sc.Err()
}

// allowedExchanges — живая торговля реализована только для этой пары.
// Кандидаты с другими биржами отбрасываются ещё до печати карточки.
var allowedExchanges = map[string]bool{"gate": true, "bitget": true}

type tracker struct {
	seenPolls  map[string]int
	lastSeen   map[string]time.Time
	lastTraded map[string]time.Time
}

func newTracker() *tracker {
	return &tracker{
		seenPolls:  map[string]int{},
		lastSeen:   map[string]time.Time{},
		lastTraded: map[string]time.Time{},
	}
}

func (t *tracker) update(candidates []scan.Candidate) {
	now := time.Now()
	seenThisPoll := map[string]bool{}
	for _, c := range candidates {
		k := routeKey(c)
		seenThisPoll[k] = true
		t.seenPolls[k]++
		t.lastSeen[k] = now
	}
	// Спред пропал на этом опросе — сбрасываем счётчик живучести.
	for k, last := range t.lastSeen {
		if !seenThisPoll[k] && now.Sub(last) > 90*time.Second {
			t.seenPolls[k] = 0
		}
	}
}

func routeKey(c scan.Candidate) string {
	return c.Base + "|" + c.Buy.Exchange + "->" + c.Sell.Exchange
}

func (t *tracker) cooldownOK(c scan.Candidate, cooldown time.Duration) bool {
	last, ok := t.lastTraded[routeKey(c)]
	if !ok {
		return true
	}
	return time.Since(last) >= cooldown
}

func (t *tracker) markTraded(c scan.Candidate) {
	t.lastTraded[routeKey(c)] = time.Now()
}

// qualifies — риск-фильтры живой торговли. Это НЕ то же самое, что
// instant_unproven в бумажном отчёте: здесь мы требуем запас на
// проскальзывание, доказанную комиссию вывода и подтверждённую живучесть.
func (t *tracker) qualifies(c scan.Candidate, cfg config.Config) (bool, string) {
	if !allowedExchanges[c.Buy.Exchange] || !allowedExchanges[c.Sell.Exchange] {
		return false, "живая торговля реализована только для gate/bitget"
	}
	if cfg.RequireFeeKnown && !c.FeeKnown {
		return false, "комиссия вывода — оценка, а не факт биржи"
	}
	if c.Thin {
		return false, "стакан слишком тонкий на нужный нотионал"
	}
	if c.Buy.WithdrawEnabled == nil || !*c.Buy.WithdrawEnabled {
		return false, "вывод с биржи покупки не подтверждён открытым"
	}
	if c.Sell.DepositEnabled == nil || !*c.Sell.DepositEnabled {
		return false, "депозит на бирже продажи не подтверждён открытым"
	}
	netAfterBuffer := c.NetPct - cfg.SlippageBufferPct
	if netAfterBuffer <= 0 {
		return false, fmt.Sprintf("net %.3f%% не покрывает буфер на проскальзывание %.2f%%", c.NetPct, cfg.SlippageBufferPct)
	}
	if t.seenPolls[routeKey(c)] < cfg.PersistPolls {
		return false, fmt.Sprintf("спред виден %d опрос(ов) подряд, нужно %d", t.seenPolls[routeKey(c)], cfg.PersistPolls)
	}
	if !t.cooldownOK(c, time.Duration(cfg.CooldownMinutes)*time.Minute) {
		return false, "монета на cooldown после недавней сделки"
	}
	return true, ""
}

func askYesNo(prompt string) bool {
	fmt.Print(prompt + " [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes" || line == "да"
}

func main() {
	cfgPath := flag.String("config", "configs/tradebot_100.yaml", "YAML-конфиг")
	interval := flag.Duration("interval", 30*time.Second, "пауза между опросами")
	execute := flag.Bool("execute", false, "реально вызывать API бирж (без флага — бумага, только печать карточек)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = loadEnv(".env")

	gate := exch.GateAuth{Key: os.Getenv("GATE_API_KEY"), Secret: os.Getenv("GATE_API_SECRET")}
	bitget := exch.BitgetAuth{
		Key:        os.Getenv("BITGET_API_KEY"),
		Secret:     os.Getenv("BITGET_API_SECRET"),
		Passphrase: os.Getenv("BITGET_API_PASSPHRASE"),
	}
	if *execute {
		if gate.Key == "" || gate.Secret == "" || bitget.Key == "" || bitget.Secret == "" || bitget.Passphrase == "" {
			fmt.Fprintln(os.Stderr, "--execute требует все ключи в .env: GATE_API_KEY/SECRET, BITGET_API_KEY/SECRET/PASSPHRASE")
			os.Exit(1)
		}
	}

	mode := "БУМАГА (--execute не передан, реальных вызовов не будет)"
	if *execute {
		mode = "ЖИВАЯ ТОРГОВЛЯ — реальные деньги"
	}
	fmt.Printf("tradebot: режим %s | нотионал $%.0f (макс. $%.0f за сделку) | persist_polls=%d | cooldown=%dмин | буфер проскальзывания %.2f%%\n",
		mode, cfg.NotionalUSD, cfg.MaxNotionalPerTradeUSD, cfg.PersistPolls, cfg.CooldownMinutes, cfg.SlippageBufferPct)

	tr := newTracker()
	for {
		res, err := scan.Run(cfg, false, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ошибка опроса:", err)
			time.Sleep(*interval)
			continue
		}
		tr.update(res.Instant)

		for _, c := range res.Instant {
			ok, _ := tr.qualifies(c, cfg)
			if !ok {
				continue
			}
			printTicket(c, cfg)
			if !askYesNo("Проводить сделку?") {
				fmt.Println("пропущено пользователем")
				continue
			}
			tr.markTraded(c)
			if err := runTrade(c, cfg, gate, bitget, *execute); err != nil {
				fmt.Fprintln(os.Stderr, "ошибка сделки:", err)
			}
		}

		time.Sleep(*interval)
	}
}

func printTicket(c scan.Candidate, cfg config.Config) {
	feeSrc := "оценка сети"
	if c.FeeKnown {
		feeSrc = "биржа (факт)"
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("СДЕЛКА: %s  купить на %s по %s  →  продать на %s по %s\n",
		c.Base, c.Buy.Exchange, fmtPrice(c.Buy.AskUSD), c.Sell.Exchange, fmtPrice(c.Sell.BidUSD))
	fmt.Printf("  Сеть: %s | ETA перевода: ~%d мин | Нотионал: $%.0f\n", c.Chain, c.ETAMin, cfg.NotionalUSD)
	fmt.Printf("  Gross: %.3f%% | Net: %.3f%% ($%.2f) | Комиссия вывода: $%.2f [%s]\n",
		c.GrossPct, c.NetPct, c.NetUSD, c.FeeUSD, feeSrc)
	fmt.Printf("  После буфера на проскальзывание (%.2f%%): ~%.3f%%\n", cfg.SlippageBufferPct, c.NetPct-cfg.SlippageBufferPct)
	fmt.Println(strings.Repeat("-", 60))
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

// runTrade проводит одну сделку с подтверждением перед выводом и перед продажей.
// Каждый шаг без --execute только печатает, что бы произошло.
func runTrade(c scan.Candidate, cfg config.Config, gate exch.GateAuth, bitget exch.BitgetAuth, execute bool) error {
	notional := cfg.NotionalUSD
	if notional > cfg.MaxNotionalPerTradeUSD {
		notional = cfg.MaxNotionalPerTradeUSD
	}

	// Шаг 1: покупка.
	fmt.Printf("[1/3] Покупка на %s: market buy на $%.0f %s ...\n", c.Buy.Exchange, notional, c.Base)
	baseBought, err := marketBuy(c.Buy.Exchange, c.Buy.Symbol, notional, gate, bitget, execute)
	if err != nil {
		return fmt.Errorf("покупка: %w", err)
	}
	fmt.Printf("    куплено ~%s %s\n", baseBought, c.Base)

	// Шаг 2: вывод — отдельное подтверждение, это необратимо.
	if !askYesNo(fmt.Sprintf("[2/3] Вывести %s %s с %s на %s (сеть %s)?", baseBought, c.Base, c.Buy.Exchange, c.Sell.Exchange, c.Sell.Chain)) {
		fmt.Println("    вывод отменён пользователем — монета остаётся на бирже покупки")
		return nil
	}
	if err := withdrawAndWait(c, baseBought, gate, bitget, execute); err != nil {
		return fmt.Errorf("вывод/ожидание депозита: %w", err)
	}

	// Шаг 3: продажа — отдельное подтверждение.
	if !askYesNo(fmt.Sprintf("[3/3] Продать %s %s на %s?", baseBought, c.Base, c.Sell.Exchange)) {
		fmt.Println("    продажа отменена пользователем — монета осталась на бирже назначения")
		return nil
	}
	if err := marketSell(c.Sell.Exchange, c.Sell.Symbol, baseBought, gate, bitget, execute); err != nil {
		return fmt.Errorf("продажа: %w", err)
	}
	fmt.Println("    готово")
	return nil
}

func marketBuy(exchange, symbol string, quoteUSD float64, gate exch.GateAuth, bitget exch.BitgetAuth, execute bool) (string, error) {
	amount := strconv.FormatFloat(quoteUSD, 'f', 2, 64)
	if !execute {
		return "N/A (бумага)", nil
	}
	switch exchange {
	case "gate":
		res, err := gate.MarketBuy(symbol, amount)
		if err != nil {
			return "", err
		}
		return res.FilledTot, nil
	case "bitget":
		res, err := bitget.MarketBuy(symbol, amount)
		if err != nil {
			return "", err
		}
		return res.OrderID, nil
	default:
		return "", fmt.Errorf("неизвестная биржа %s", exchange)
	}
}

func marketSell(exchange, symbol, baseAmount string, gate exch.GateAuth, bitget exch.BitgetAuth, execute bool) error {
	if !execute {
		fmt.Printf("    бумага: был бы market sell %s %s на %s\n", baseAmount, symbol, exchange)
		return nil
	}
	switch exchange {
	case "gate":
		res, err := gate.MarketSell(symbol, baseAmount)
		if err != nil {
			return err
		}
		fmt.Printf("    ордер %s: статус=%s выручка=%s средняя цена=%s\n", res.ID, res.Status, res.FilledTot, res.AvgDealPr)
		return nil
	case "bitget":
		_, err := bitget.MarketSell(symbol, baseAmount)
		return err
	default:
		return fmt.Errorf("неизвестная биржа %s", exchange)
	}
}

// withdrawAndWait выводит монету с биржи покупки и ждёт зачисления на бирже
// продажи (poll баланса), с таймаутом ~4×ETA.
func withdrawAndWait(c scan.Candidate, baseAmount string, gate exch.GateAuth, bitget exch.BitgetAuth, execute bool) error {
	if !execute {
		fmt.Printf("    бумага: был бы вывод %s %s (%s → %s, сеть %s), ожидание ~%d мин\n",
			baseAmount, c.Base, c.Buy.Exchange, c.Sell.Exchange, c.Sell.Chain, c.ETAMin)
		return nil
	}

	var address string
	switch c.Sell.Exchange {
	case "gate":
		addr, err := gate.DepositAddress(c.Base)
		if err != nil {
			return fmt.Errorf("адрес депозита на gate: %w", err)
		}
		address = addr.Address
		for _, m := range addr.MultichainAddresses {
			if strings.EqualFold(m.Chain, c.Sell.Chain) {
				address = m.Address
				break
			}
		}
	case "bitget":
		addr, err := bitget.DepositAddress(c.Base, c.Sell.Chain)
		if err != nil {
			return fmt.Errorf("адрес депозита на bitget: %w", err)
		}
		address = addr.Address
	default:
		return fmt.Errorf("неизвестная биржа назначения %s", c.Sell.Exchange)
	}
	if address == "" {
		return fmt.Errorf("не удалось получить адрес депозита на %s для %s/%s", c.Sell.Exchange, c.Base, c.Sell.Chain)
	}

	fmt.Printf("    вызываю вывод: %s %s → %s (сеть %s)\n", baseAmount, c.Base, address, c.Buy.Chain)
	switch c.Buy.Exchange {
	case "gate":
		if _, err := gate.Withdraw(c.Base, c.Buy.Chain, address, baseAmount); err != nil {
			return fmt.Errorf("вывод с gate: %w", err)
		}
	case "bitget":
		if _, err := bitget.Withdraw(c.Base, c.Buy.Chain, address, baseAmount); err != nil {
			return fmt.Errorf("вывод с bitget: %w", err)
		}
	default:
		return fmt.Errorf("неизвестная биржа источника %s", c.Buy.Exchange)
	}

	timeout := time.Duration(c.ETAMin*4) * time.Minute
	deadline := time.Now().Add(timeout)
	fmt.Printf("    жду зачисления на %s (таймаут %v)...\n", c.Sell.Exchange, timeout)
	for time.Now().Before(deadline) {
		var avail float64
		switch c.Sell.Exchange {
		case "gate":
			b, err := gate.Balance(c.Base)
			if err == nil {
				avail, _ = strconv.ParseFloat(b.Available, 64)
			}
		case "bitget":
			b, err := bitget.Balance(c.Base)
			if err == nil {
				avail, _ = strconv.ParseFloat(b.Available, 64)
			}
		}
		wantRaw, _ := strconv.ParseFloat(baseAmount, 64)
		if avail >= wantRaw*0.9 { // допуск на комиссию сети, списанную из суммы
			fmt.Printf("    зачислено: доступно %.8f %s на %s\n", avail, c.Base, c.Sell.Exchange)
			return nil
		}
		time.Sleep(30 * time.Second)
	}
	return fmt.Errorf("не дождались зачисления за %v — проверьте вручную перед продажей", timeout)
}
