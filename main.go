package main

import (
	"fmt"
	"log"

	"github.com/godaddy-x/wallet-adapter-btc/btc"
)

func main() {
	jsonContent := `{
		"serverAPI": "http://127.0.0.1:8332",
		"rpcUser": "user",
		"rpcPassword": "password",
		"rpcServerType": "0",
		"isTestNet": "false",
		"supportSegWit": "true",
		"minFees": "0.00001",
		"dataDir": "data"
	}`
	adapter, err := btc.NewAdapter(jsonContent, "BTC", "Bitcoin", 8)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("btc adapter ready: symbol=%s fullName=%s decimals=%d\n", adapter.Symbol(), adapter.FullName(), adapter.Decimal())
}
