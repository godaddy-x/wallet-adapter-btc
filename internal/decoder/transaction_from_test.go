package decoder

import "testing"

func TestAggregateAddrAmountLegs(t *testing.T) {
	addr := "bcrt1qpayer"
	addrs := []string{addr, addr, addr}
	amts := []string{"6.25", "6.25", "6.24954326"}
	gotAddrs, gotAmts := aggregateAddrAmountLegs(addrs, amts)
	if len(gotAddrs) != 1 || gotAddrs[0] != addr {
		t.Fatalf("addrs = %v, want single payer", gotAddrs)
	}
	if len(gotAmts) != 1 || gotAmts[0] != "18.74954326" {
		t.Fatalf("amts = %v, want total 18.74954326", gotAmts)
	}
	lines := joinAddrAmount(gotAddrs, gotAmts)
	if len(lines) != 1 || lines[0] != addr+":18.74954326" {
		t.Fatalf("lines = %v", lines)
	}
}
