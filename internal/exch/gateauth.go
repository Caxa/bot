package exch

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// GateAuth — подписанный клиент Gate API v4 (торговый счёт, без вывода).
type GateAuth struct {
	Key    string
	Secret string
}

const gateBase = "https://api.gateio.ws"

func (g GateAuth) sign(method, path, query, body string) (ts, sign string) {
	ts = strconv.FormatInt(time.Now().Unix(), 10)
	h := sha512.Sum512([]byte(body))
	bodyHash := hex.EncodeToString(h[:])
	msg := method + "\n" + path + "\n" + query + "\n" + bodyHash + "\n" + ts
	mac := hmac.New(sha512.New, []byte(g.Secret))
	mac.Write([]byte(msg))
	return ts, hex.EncodeToString(mac.Sum(nil))
}

func (g GateAuth) do(method, path, query, body string, out any) error {
	ts, sign := g.sign(method, path, query, body)
	url := gateBase + path
	if query != "" {
		url += "?" + query
	}
	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("KEY", g.Key)
	req.Header.Set("Timestamp", ts)
	req.Header.Set("SIGN", sign)
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gate api %s %s: статус %d: %s", method, path, resp.StatusCode, string(raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("разбор ответа %s: %w (тело: %s)", path, err, string(raw))
		}
	}
	return nil
}

type GateBalance struct {
	Currency  string `json:"currency"`
	Available string `json:"available"`
	Locked    string `json:"locked"`
}

// Balance возвращает баланс одной валюты на споте (0 если валюты нет в ответе).
func (g GateAuth) Balance(currency string) (GateBalance, error) {
	var out []GateBalance
	if err := g.do("GET", "/api/v4/spot/accounts", "currency="+currency, "", &out); err != nil {
		return GateBalance{}, err
	}
	for _, b := range out {
		if b.Currency == currency {
			return b, nil
		}
	}
	return GateBalance{Currency: currency, Available: "0", Locked: "0"}, nil
}

type GateOrderResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Side       string `json:"side"`
	Amount     string `json:"amount"`
	Price      string `json:"price"`
	FilledTot  string `json:"filled_total"`
	Pair       string `json:"currency_pair"`
	AvgDealPr  string `json:"avg_deal_price"`
	Fee        string `json:"fee"`
	FeeCcy     string `json:"fee_currency"`
	CreateTime string `json:"create_time"`
}

// MarketSell продаёт amount базовой валюты пары по рынку (IOC).
func (g GateAuth) MarketSell(pair, amount string) (GateOrderResult, error) {
	body, _ := json.Marshal(map[string]string{
		"currency_pair": pair,
		"side":          "sell",
		"type":          "market",
		"amount":        amount,
		"time_in_force": "ioc",
	})
	var out GateOrderResult
	err := g.do("POST", "/api/v4/spot/orders", "", string(body), &out)
	return out, err
}

// MarketBuy покупает на quoteAmount (в USDT) базовой валюты пары по рынку.
// На Gate для market buy поле amount — это сумма в квот-валюте.
func (g GateAuth) MarketBuy(pair, quoteAmount string) (GateOrderResult, error) {
	body, _ := json.Marshal(map[string]string{
		"currency_pair": pair,
		"side":          "buy",
		"type":          "market",
		"amount":        quoteAmount,
		"time_in_force": "ioc",
	})
	var out GateOrderResult
	err := g.do("POST", "/api/v4/spot/orders", "", string(body), &out)
	return out, err
}

type GateDepositAddress struct {
	Currency            string `json:"currency"`
	Address             string `json:"address"`
	MultichainAddresses []struct {
		Chain   string `json:"chain"`
		Address string `json:"address"`
	} `json:"multichain_addresses"`
}

// DepositAddress получает адрес(а) пополнения для currency (может быть мультичейн).
func (g GateAuth) DepositAddress(currency string) (GateDepositAddress, error) {
	var out GateDepositAddress
	err := g.do("GET", "/api/v4/wallet/deposit_address", "currency="+currency, "", &out)
	return out, err
}

type GateWithdrawResult struct {
	ID string `json:"id"`
}

// Withdraw выводит amount валюты currency в сети chain на address. Необратимо.
func (g GateAuth) Withdraw(currency, chain, address, amount string) (GateWithdrawResult, error) {
	body, _ := json.Marshal(map[string]string{
		"currency": currency,
		"address":  address,
		"amount":   amount,
		"chain":    chain,
	})
	var out GateWithdrawResult
	err := g.do("POST", "/api/v4/withdrawals", "", string(body), &out)
	return out, err
}
