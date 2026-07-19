module github.com/godaddy-x/wallet-adapter-btc

go 1.26

require (
	github.com/btcsuite/btcd v0.23.5-0.20231215221805-96c9fd8078fd
	github.com/btcsuite/btcd/btcec/v2 v2.1.3
	github.com/btcsuite/btcd/btcutil v1.1.5
	github.com/btcsuite/btcd/chaincfg/chainhash v1.1.0
	github.com/godaddy-x/wallet-adapter v1.0.9
	github.com/imroc/req v0.3.2
	github.com/mailru/easyjson v0.9.1
	github.com/shopspring/decimal v1.4.0
	github.com/tidwall/gjson v1.18.0
	go.mongodb.org/mongo-driver v1.17.3
)

require (
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f // indirect
	github.com/btcsuite/btcutil v1.0.3-0.20201208143702-a53e38424cce // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/klauspost/compress v1.16.7 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.35.0 // indirect
)

replace github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.22.1

//replace github.com/godaddy-x/wallet-adapter => ../../github/wallet-adapter
