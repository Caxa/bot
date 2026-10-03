package exch

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// BitgetAuth — подписанный клиент Bitget spot API v2 (ордера, баланс, вывод).
type BitgetAuth struct {
	Key        string
	Secret     string
	Passphrase string
}

const bitgetBase = "https://api.bitget.com"

func (b BitgetAuth) sign(ts, method, requestPath, body string) string {
	msg := ts + method + requestPath + body
	mac := hmac.New(sha256.New, []byte(b.Secret))
	mac.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (b BitgetAuth) do(method, path, query, body string, out any) error {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	requestPath := path
	if query != "" {
		requestPath += "?" + query
	}
	sign := b.sign(ts, method, requestPath, body)
	url := bitgetBase + requestPath
	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("ACCESS-KEY", b.Key)
	req.Header.Set("ACCESS-SIGN", sign)
	req.Header.Set("ACCESS-TIMESTAMP", ts)
	req.Header.Set("ACCESS-PASSPHRASE", b.Passphrase)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "en-US")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bitget api %s %s: статус %d: %s", method, path, resp.StatusCode, string(raw))
	}
	var env struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("разбор ответа %s: %w (тело: %s)", path, err, string(raw))
	}
	if env.Code != "" && env.Code != "00000" {
		return fmt.Errorf("bitget api %s %s: code=%s msg=%s", method, path, env.Code, env.Msg)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("разбор data %s: %w (тело: %s)", path, err, string(raw))
		}
	}
	return nil
}

type BitgetBalance struct {
	Coin      string `json:"coin"`
	Available string `json:"available"`
	Frozen    string `json:"frozen"`
	Locked    string `json:"locked"`
}

// Balance возвращает баланс одной монеты на споте (0 если монеты нет в ответе).
func (b BitgetAuth) Balance(coin string) (BitgetBalance, error) {
	var out []BitgetBalance
	if err := b.do("GET", "/api/v2/spot/account/assets", "coin="+coin, "", &out); err != nil {
		return BitgetBalance{}, err
	}
	for _, x := range out {
		if x.Coin == coin {
			return x, nil
		}
	}
	return BitgetBalance{Coin: coin, Available: "0"}, nil
}

type BitgetOrderResult struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

// MarketBuy покупает на quoteAmount (в USDT) базовой валюты пары по рынку.
// На Bitget для market buy поле size — это сумма в квот-валюте.
func (b BitgetAuth) MarketBuy(symbol, quoteAmount string) (BitgetOrderResult, error) {
	return b.placeOrder(symbol, "buy", quoteAmount)
}

// MarketSell продаёт baseAmount базовой валюты пары по рынку.
// На Bitget для market sell поле size — это количество в базовой валюте.
func (b BitgetAuth) MarketSell(symbol, baseAmount string) (BitgetOrderResult, error) {
	return b.placeOrder(symbol, "sell", baseAmount)
}

func (b BitgetAuth) placeOrder(symbol, side, size string) (BitgetOrderResult, error) {
	body, _ := json.Marshal(map[string]string{
		"symbol":    symbol,
		"side":      side,
		"orderType": "market",
		"force":     "gtc",
		"size":      size,
	})
	var out BitgetOrderResult
	err := b.do("POST", "/api/v2/spot/trade/place-order", "", string(body), &out)
	return out, err
}

type BitgetOrderInfo struct {
	OrderID     string `json:"orderId"`
	Status      string `json:"status"`
	BaseVolume  string `json:"baseVolume"`
	QuoteVolume string `json:"quoteVolume"`
	PriceAvg    string `json:"priceAvg"`
	Fee         string `json:"fee"`
}

// OrderInfo возвращает текущее состояние ордера (для ожидания исполнения).
func (b BitgetAuth) OrderInfo(symbol, orderID string) (BitgetOrderInfo, error) {
	var out []BitgetOrderInfo
	q := fmt.Sprintf("symbol=%s&orderId=%s", symbol, orderID)
	if err := b.do("GET", "/api/v2/spot/trade/fills", q, "", &out); err != nil {
		return BitgetOrderInfo{}, err
	}
	if len(out) == 0 {
		return BitgetOrderInfo{OrderID: orderID, Status: "unknown"}, nil
	}
	return out[0], nil
}

type BitgetDepositAddress struct {
	Address string `json:"address"`
	Chain   string `json:"chain"`
	Tag     string `json:"tag"`
}

// DepositAddress получает адрес для пополнения coin в сети chain.
func (b BitgetAuth) DepositAddress(coin, chain string) (BitgetDepositAddress, error) {
	var out BitgetDepositAddress
	q := fmt.Sprintf("coin=%s&chain=%s", coin, chain)
	err := b.do("GET", "/api/v2/spot/wallet/deposit-address", q, "", &out)
	return out, err
}

type BitgetWithdrawResult struct {
	OrderID string `json:"orderId"`
}

// Withdraw выводит amount монеты coin в сети chain на address. Необратимо.
func (b BitgetAuth) Withdraw(coin, chain, address, amount string) (BitgetWithdrawResult, error) {
	body, _ := json.Marshal(map[string]string{
		"coin":         coin,
		"transferType": "on_chain",
		"chain":        chain,
		"address":      address,
		"size":         amount,
	})
	var out BitgetWithdrawResult
	err := b.do("POST", "/api/v2/spot/wallet/withdrawal", "", string(body), &out)
	return out, err
}
