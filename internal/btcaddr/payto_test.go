package btcaddr

import "testing"

func TestPayToAddressScriptRegtest(t *testing.T) {
	for _, addr := range []string{
		"bcrt1qzqwuj487l2weae4vqpdqfku5gk7ssj8h5ry6ec",
		"bcrt1qay6v8dmyqu6lu6z448fx9re0c5nzy2ye22shua",
	} {
		script, err := PayToAddressScript(addr, "regtest")
		if err != nil {
			t.Fatalf("addr %s: %v", addr, err)
		}
		if len(script) == 0 {
			t.Fatalf("empty script for %s", addr)
		}
	}
}
