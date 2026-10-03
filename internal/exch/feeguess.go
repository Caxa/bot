package exch

import "strings"

// approxChainFeeUSD — грубая оценка комиссии вывода по сети в долларах, ТОЛЬКО
// для бирж, которые не отдают реальную комиссию через публичный API без ключа
// (например Gate: /spot/currencies не содержит fee, а /wallet/withdraw_status
// требует подписанный запрос). Цифры примерные (по газу сети + типичной
// наценке биржи на конец 2025), меняются без предупреждения и НЕ заменяют
// проверку на сайте биржи перед реальным выводом.
var approxChainFeeUSD = map[string]float64{
	"BSC":     0.30,
	"BEP20":   0.30,
	"ETH":     3.00,
	"ERC20":   3.00,
	"TRX":     1.00,
	"TRC20":   1.00,
	"SOL":     0.02,
	"POLYGON": 0.02,
	"MATIC":   0.02,
	"TON":     0.05,
	"ARB":     0.10,
	"BTC":     1.50,
}

// ApproxWithdrawFeeUSD возвращает грубую оценку и true, если для этой сети
// она вообще есть. Использовать только как нижнюю границу "точно спишут не
// меньше", когда биржа не раскрыла реальную комиссию.
func ApproxWithdrawFeeUSD(chain string) (float64, bool) {
	v, ok := approxChainFeeUSD[strings.ToUpper(strings.TrimSpace(chain))]
	return v, ok
}
