package pipeline

// Состояния одной сделки «купил → перевёл → продал».
// Живой вывод не вызывается из бумаги; симулятор только двигает статус.
type State string

const (
	Planned     State = "planned"
	Bought      State = "bought"
	Hedged      State = "hedged"
	Withdrawing State = "withdrawing"
	InFlight    State = "in_flight"
	Deposited   State = "deposited"
	Sold        State = "sold"
	HedgeClosed State = "hedge_closed"
)

type Deal struct {
	ID           string
	Coin         string
	Chain        string
	Contract     string
	BuyExchange  string
	SellExchange string
	State        State
	Note         string
}

// PaperAdvance — следующий шаг без биржевых вызовов.
func PaperAdvance(d Deal) Deal {
	switch d.State {
	case Planned:
		d.State = Bought
		d.Note = "paper: would buy on " + d.BuyExchange
	case Bought:
		d.State = Hedged
		d.Note = "paper: would short-perp hedge while coins travel"
	case Hedged:
		d.State = Withdrawing
		d.Note = "paper: would withdraw (API not called)"
	case Withdrawing:
		d.State = InFlight
		d.Note = "paper: waiting conservative ETA"
	case InFlight:
		d.State = Deposited
		d.Note = "paper: would credit on " + d.SellExchange
	case Deposited:
		d.State = Sold
		d.Note = "paper: would sell"
	case Sold:
		d.State = HedgeClosed
		d.Note = "paper: would close hedge"
	}
	return d
}
