package cryptochief

import "context"

// WalletsService groups wallet-management endpoints. Access via
// Client.Wallets.
type WalletsService struct{ c *Client }

// WalletType is the role of a project wallet.
type WalletType string

const (
	WalletTypeMaster  WalletType = "master"
	WalletTypeTransit WalletType = "transit"
	WalletTypeStatic  WalletType = "static"
)

// GenerateWalletRequest is the body of POST /v1/wallets/generate.
type GenerateWalletRequest struct {
	WalletType          WalletType  `json:"wallet_type"`
	ChainFamily         ChainFamily `json:"chain_family"`
	MasterWalletAddress string      `json:"master_wallet_address,omitempty"` // transit/static only
	CallbackURL         string      `json:"callback_url,omitempty"`          // static only
}

// Wallet is the merchant-side view of one wallet.
type Wallet struct {
	Address             string      `json:"address"`
	ChainFamily         ChainFamily `json:"chain_family"`
	Type                WalletType  `json:"type,omitempty"`
	WalletType          WalletType  `json:"wallet_type,omitempty"`
	Frozen              bool        `json:"frozen,omitempty"`
	MasterWalletAddress string      `json:"master_wallet_address,omitempty"`
	CallbackURL         string      `json:"callback_url,omitempty"`
	PrivateKeyEncrypted string      `json:"private_key_encrypted,omitempty"`
	CreatedAt           string      `json:"created_at,omitempty"`

	// Populated by /v1/wallets/info.
	Coins           []WalletCoinBalance `json:"coins,omitempty"`
	TotalBalanceUSD string              `json:"total_balance_usd,omitempty"`
}

// WalletCoinBalance is one per-coin row in /wallets/info.
type WalletCoinBalance struct {
	Address    string `json:"address"`
	Chain      Chain  `json:"chain"`
	Coin       string `json:"coin"`
	Contract   string `json:"contract,omitempty"`
	Decimals   int    `json:"decimals"`
	Value      string `json:"value"`
	HumanValue string `json:"human_value"`
	AmountUSD  string `json:"amount_usd,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
}

// ListWalletsResponse is the response of /v1/wallets/list.
type ListWalletsResponse struct {
	Items []Wallet `json:"items"`
}

// Generate provisions a new wallet on the requested chain family. master
// wallets are root-of-trust; transit and static wallets attach to a master.
// Static wallets get a fixed deposit address per-customer (with optional
// callback_url for per-deposit webhooks).
func (s *WalletsService) Generate(ctx context.Context, in *GenerateWalletRequest) (*Wallet, error) {
	var out Wallet
	if err := s.c.do(ctx, "/v1/wallets/generate", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns every wallet on the project.
func (s *WalletsService) List(ctx context.Context) (*ListWalletsResponse, error) {
	var out ListWalletsResponse
	if err := s.c.do(ctx, "/v1/wallets/list", struct{}{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Info fetches details and current balances of one wallet.
func (s *WalletsService) Info(ctx context.Context, address string) (*Wallet, error) {
	var out Wallet
	if err := s.c.do(ctx, "/v1/wallets/info", map[string]string{"address": address}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Freeze toggles the frozen flag on a wallet — frozen wallets are excluded
// from payout source selection and sweeping. The endpoint is a toggle; the
// response's "frozen" field tells you the new state.
func (s *WalletsService) Freeze(ctx context.Context, address string) (*Wallet, error) {
	var out Wallet
	if err := s.c.do(ctx, "/v1/wallets/freeze", map[string]string{"address": address}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DecryptPrivateKey decrypts the `private_key_encrypted` field of a
// generated wallet using the RSA private key configured on the [Client]
// via [WithRSAPrivateKey] / [WithRSAPrivateKeyPEM]. The result is the
// chain-native hex form of the wallet's raw private key.
//
// Returns [ErrRSAKeyNotConfigured] if no key was loaded. Returns the
// underlying load error if the configured key failed to parse at init.
//
//	w, _ := c.Wallets.Generate(ctx, &cryptochief.GenerateWalletRequest{
//	    WalletType:  cryptochief.WalletTypeMaster,
//	    ChainFamily: cryptochief.FamilyEVM,
//	})
//	privHex, err := c.Wallets.DecryptPrivateKey(w.PrivateKeyEncrypted)
func (s *WalletsService) DecryptPrivateKey(encrypted string) (string, error) {
	if s.c.rsaInitErr != nil {
		return "", s.c.rsaInitErr
	}
	if s.c.rsaPrivateKey == nil {
		return "", ErrRSAKeyNotConfigured
	}
	return DecryptRSAOAEP(s.c.rsaPrivateKey, encrypted)
}
