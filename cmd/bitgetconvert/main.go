// Command bitgetconvert продаёт одну монету на Bitget по рынку за USDT.
// Без -execute только показывает баланс и ничего не делает (бумага).
// Ключи берутся из .env (BITGET_API_KEY/SECRET/PASSPHRASE), в чат/репозиторий не попадают.
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
	key := os.Getenv("BITGET_API_KEY")
	secret := os.Getenv("BITGET_API_SECRET")
	pass := os.Getenv("BITGET_API_PASSPHRASE")
	if key == "" || secret == "" || pass == "" {
		fmt.Fprintln(os.Stderr, "нет BITGET_API_KEY / BITGET_API_SECRET / BITGET_API_PASSPHRASE — проверьте .env")
		os.Exit(1)
	}
	b := exch.BitgetAuth{Key: key, Secret: secret, Passphrase: pass}

	coinU := strings.ToUpper(*coin)
	bal, err := b.Balance(coinU)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка баланса:", err)
		os.Exit(1)
	}
	fmt.Printf("баланс %s на Bitget: доступно %s, заморожено %s\n", bal.Coin, bal.Available, bal.Frozen)

	avail, _ := strconv.ParseFloat(bal.Available, 64)
	if avail <= 0 {
		fmt.Println("продавать нечего — доступный баланс 0")
		return
	}

	symbol := coinU + "USDT"
	if !*execute {
		fmt.Printf("бумага: без -execute ордер не выставляется. Был бы: market sell %s %s\n", bal.Available, symbol)
		return
	}

	fmt.Printf("ВЫСТАВЛЯЮ реальный ордер: market sell %s %s ...\n", bal.Available, symbol)
	res, err := b.MarketSell(symbol, bal.Available)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка ордера:", err)
		os.Exit(1)
	}
	fmt.Printf("ордер %s выставлен\n", res.OrderID)
}
