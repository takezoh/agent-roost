package transcript

type TurnText struct {
	Role string
	Text string
}

const recentTurnsCap = 64

func appendRecentTurn(turns []TurnText, role, text string) []TurnText {
	if text == "" {
		return turns
	}
	turns = append(turns, TurnText{Role: role, Text: text})
	if len(turns) <= recentTurnsCap {
		return turns
	}
	out := make([]TurnText, recentTurnsCap)
	copy(out, turns[len(turns)-recentTurnsCap:])
	return out
}
