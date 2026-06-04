package cryptochief

// Chain is a Crypto Chief CHAIN code (the value of "network" / "chain" /
// "network_code" fields across the API).
type Chain string

// All chains the API currently supports. The mainnet/testnet split is
// purely by name; ChainFamily() tells you what protocol family they belong
// to (used for capability checks like "does this chain support contract
// calls?").
const (
	ChainEthMainnet      Chain = "ETH_MAINNET"
	ChainEthSepolia      Chain = "ETH_SEPOLIA"
	ChainBSCMainnet      Chain = "BSC_MAINNET"
	ChainBSCTestnet      Chain = "BSC_TESTNET"
	ChainPolygonMainnet  Chain = "POLYGON_MAINNET"
	ChainPolygonAmoy     Chain = "POLYGON_AMOY"
	ChainArbitrumOne     Chain = "ARBITRUM_ONE"
	ChainArbitrumSepolia Chain = "ARBITRUM_SEPOLIA"
	ChainOptimismMainnet Chain = "OPTIMISM_MAINNET"
	ChainOptimismSepolia Chain = "OPTIMISM_SEPOLIA"
	ChainAvaxMainnet     Chain = "AVAX_MAINNET"
	ChainAvaxTestnet     Chain = "AVAX_TESTNET"

	ChainBTCMainnet  Chain = "BTC_MAINNET"
	ChainBTCTestnet  Chain = "BTC_TESTNET_4"
	ChainLitecoin    Chain = "LITECOIN_MAINNET"
	ChainBitcoinCash Chain = "BITCOIN_CASH_MAINNET"
	ChainDogecoin    Chain = "DOGECOIN_MAINNET"

	ChainTronMainnet Chain = "TRON_MAINNET"
	ChainTronNile    Chain = "TRON_NILE"

	ChainSolanaMainnet Chain = "SOLANA_MAINNET"
	ChainSolanaDevnet  Chain = "SOLANA_DEVNET"

	ChainTONMainnet Chain = "TON_MAINNET"
	ChainTONTestnet Chain = "TON_TESTNET"

	ChainXRPMainnet Chain = "XRP_MAINNET"
	ChainXRPTestnet Chain = "XRP_TESTNET"
)

// ChainFamily groups chains by underlying protocol (the value of
// "chain_family" in API responses). Capability differs by family (e.g.
// only EVM/TRON/SOLANA/TON accept contract calls in the two-phase
// sign/execute flow).
type ChainFamily string

const (
	FamilyEVM             ChainFamily = "EVM"
	FamilyTRON            ChainFamily = "TRON"
	FamilySolana          ChainFamily = "SOLANA"
	FamilyXRPLedger       ChainFamily = "XRP_LEDGER"
	FamilyTON             ChainFamily = "TON"
	FamilyBTCUTXO         ChainFamily = "BTC_UTXO"
	FamilyBTCUTXOTestnet  ChainFamily = "BTC_UTXO_TESTNET"
	FamilyDogecoinUTXO    ChainFamily = "DOGECOIN_UTXO"
	FamilyBitcoinCashUTXO ChainFamily = "BTC_CASH_UTXO"
	FamilyLitecoinUTXO    ChainFamily = "LITECOIN_UTXO"
)

// chainToFamily maps every supported chain to its family.
var chainToFamily = map[Chain]ChainFamily{
	ChainEthMainnet:      FamilyEVM,
	ChainEthSepolia:      FamilyEVM,
	ChainBSCMainnet:      FamilyEVM,
	ChainBSCTestnet:      FamilyEVM,
	ChainPolygonMainnet:  FamilyEVM,
	ChainPolygonAmoy:     FamilyEVM,
	ChainArbitrumOne:     FamilyEVM,
	ChainArbitrumSepolia: FamilyEVM,
	ChainOptimismMainnet: FamilyEVM,
	ChainOptimismSepolia: FamilyEVM,
	ChainAvaxMainnet:     FamilyEVM,
	ChainAvaxTestnet:     FamilyEVM,

	ChainBTCMainnet:  FamilyBTCUTXO,
	ChainBTCTestnet:  FamilyBTCUTXOTestnet,
	ChainLitecoin:    FamilyLitecoinUTXO,
	ChainBitcoinCash: FamilyBitcoinCashUTXO,
	ChainDogecoin:    FamilyDogecoinUTXO,

	ChainTronMainnet:   FamilyTRON,
	ChainTronNile:      FamilyTRON,
	ChainSolanaMainnet: FamilySolana,
	ChainSolanaDevnet:  FamilySolana,
	ChainTONMainnet:    FamilyTON,
	ChainTONTestnet:    FamilyTON,
	ChainXRPMainnet:    FamilyXRPLedger,
	ChainXRPTestnet:    FamilyXRPLedger,
}

// Family returns the chain family for c, or "" if c is not a recognised
// constant. Useful for client-side validation before calling the API.
func (c Chain) Family() ChainFamily {
	return chainToFamily[c]
}

// SupportsContractCalls reports whether the chain family accepts the
// transactions/signature "contract" type with a calls[] body. Only
// EVM, TRON, Solana, and TON do.
func (f ChainFamily) SupportsContractCalls() bool {
	switch f {
	case FamilyEVM, FamilyTRON, FamilySolana, FamilyTON:
		return true
	}
	return false
}
