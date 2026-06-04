package cryptochief

// Asset identifies a specific coin on a specific network. It appears inside
// asset-selection policies — the auto-convert source filter on payouts and
// the allowed-asset list on FIAT-mode pay-ins.
//
// Network takes a chain code (e.g. [ChainEthMainnet]) or the wildcard "ANY";
// Coin is the symbol (e.g. "USDT"). Either field may be left empty to mean
// "any".
type Asset struct {
	Network Chain  `json:"network,omitempty"` // chain code or "ANY"
	Coin    string `json:"coin,omitempty"`    // e.g. "USDT"
}

// AssetsPolicy is an allow/exclude filter over [Asset] entries. The zero
// value (both lists empty) means "no restriction". It is used in two places:
//
//   - Payout auto-convert source selection —
//     [EstimatePayoutRequest.AutoConvertPolicy] / [ExecutePayoutRequest.AutoConvertPolicy].
//   - Restricting which coins a FIAT-mode customer may pick on a pay-in —
//     [CreatePayInRequest.Assets].
type AssetsPolicy struct {
	Allow   []Asset `json:"allow,omitempty"`
	Exclude []Asset `json:"exclude,omitempty"`
}
