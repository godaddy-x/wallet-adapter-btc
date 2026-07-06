package decoder

// inferSubmitTxAction classifies outbound submit responses from from/to legs.
// Authoritative txAction is set later by scanner from flowType at confirm time.
func inferSubmitTxAction(fromAddrs, toAddrs []string) string {
	hasFrom := len(fromAddrs) > 0
	hasTo := len(toAddrs) > 0
	switch {
	case hasTo && !hasFrom:
		return "receive"
	case hasFrom && hasTo:
		return "send"
	case hasFrom:
		return "send"
	default:
		return "send"
	}
}
