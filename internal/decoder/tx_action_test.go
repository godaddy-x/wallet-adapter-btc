package decoder

import "testing"

func TestInferSubmitTxAction(t *testing.T) {
	if got := inferSubmitTxAction([]string{"a"}, []string{"b"}); got != "send" {
		t.Fatalf("send: got %q", got)
	}
	if got := inferSubmitTxAction(nil, []string{"b"}); got != "receive" {
		t.Fatalf("receive: got %q", got)
	}
	if got := inferSubmitTxAction([]string{"a"}, nil); got != "send" {
		t.Fatalf("send-only: got %q", got)
	}
}
