// Command gateconvert продаёт одну монету на Gate по рынку за USDT.
// Без -execute только показывает баланс и ничего не делает (бумага).
// Ключи берутся из .env (GATE_API_KEY / GATE_API_SECRET), в чат/репозиторий не попадают.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"hedge/internal/exch"
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

func main() {
	coin := flag.String("coin", "TRX", "монета для продажи (база пары)")
	execute := flag.Bool("execute", false, "реально выставить рыночный ордер (без флага — только показать баланс)")
	flag.Parse()

	_ = loadEnv(".env")
	key := os.Getenv("GATE_API_KEY")
	secret := os.Getenv("GATE_API_SECRET")
	if key == "" || secret == "" {
		fmt.Fprintln(os.Stderr, "нет GATE_API_KEY / GATE_API_SECRET — проверьте .env")
		os.Exit(1)
	}
	g := exch.GateAuth{Key: key, Secret: secret}

	bal, err := g.Balance(strings.ToUpper(*coin))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка баланса:", err)
		os.Exit(1)
	}
	fmt.Printf("баланс %s на Gate: доступно %s, заблокировано %s\n", bal.Currency, bal.Available, bal.Locked)

	avail, _ := strconv.ParseFloat(bal.Available, 64)
	if avail <= 0 {
		fmt.Println("продавать нечего — доступный баланс 0")
		return
	}

	pair := strings.ToUpper(*coin) + "_USDT"
	if !*execute {
		fmt.Printf("бумага: без -execute ордер не выставляется. Был бы: market sell %s %s\n", bal.Available, pair)
		return
	}

	fmt.Printf("ВЫСТАВЛЯЮ реальный ордер: market sell %s %s ...\n", bal.Available, pair)
	res, err := g.MarketSell(pair, bal.Available)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка ордера:", err)
		os.Exit(1)
	}
	fmt.Printf("ордер %s: статус=%s исполнено(в quote)=%s средняя цена=%s комиссия=%s %s\n",
		res.ID, res.Status, res.FilledTot, res.AvgDealPr, res.Fee, res.FeeCcy)
}
