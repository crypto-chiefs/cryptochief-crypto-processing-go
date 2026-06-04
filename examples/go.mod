// Examples live in their own module so people who import the library don't
// pull example binaries as dependencies.
module github.com/crypto-chiefs/cryptochief-crypto-processing-go/examples

go 1.20

require github.com/crypto-chiefs/cryptochief-crypto-processing-go v0.0.0

require (
	github.com/sigurn/crc16 v0.0.0-20211026045750-20ab5afb07e3 // indirect
	github.com/xssnick/tonutils-go v1.12.0 // indirect
	golang.org/x/crypto v0.32.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
)

replace github.com/crypto-chiefs/cryptochief-crypto-processing-go => ../
