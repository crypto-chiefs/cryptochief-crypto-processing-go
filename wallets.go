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

	// Label is a human-readable name for the wallet — "hot wallet EU",
	// "customer 4242". It applies to every wallet type, not only static ones,
	// and is for your own bookkeeping: it carries no routing meaning. Max 255
	// characters; left off the wire when empty.
	Label string `json:"label,omitempty"`
}

// Wallet is the merchant-side view of one wallet.
type Wallet struct {
	Address     string      `json:"address"`
	ChainFamily ChainFamily `json:"chain_family"`
	Type        WalletType  `json:"type,omitempty"`
	WalletType  WalletType  `json:"wallet_type,omitempty"`
	Frozen      bool        `json:"frozen,omitempty"`

	// MasterWalletAddress, CallbackURL and Label are always present in the
	// wallet-info shape and are JSON null when the wallet has no such value — a
	// master has no master of its own, a transit wallet never has a callback,
	// an unnamed wallet has no label. All three decode to the empty string, so
	// an empty value means "not set" rather than "the platform omitted it";
	// the platform never sends "" for a name it has.
	MasterWalletAddress string `json:"master_wallet_address,omitempty"`
	CallbackURL         string `json:"callback_url,omitempty"`
	Label               string `json:"label,omitempty"`

	PrivateKeyEncrypted string `json:"private_key_encrypted,omitempty"`
	CreatedAt           string `json:"created_at,omitempty"`

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

// WalletPayInHistoryQuery is the body of /v1/wallets/history: the pay-ins that
// used one deposit address, narrowed from the project-wide [HistoryQuery] to a
// single wallet.
//
// Address is required. It is matched case-insensitively, so either spelling of
// an EVM address works.
type WalletPayInHistoryQuery struct {
	Address string `json:"address"`

	// DateFrom and DateTo bound the order's creation date, formatted
	// YYYY-MM-DDTHH:MM:SS±HH:MM — the same format the other history endpoints
	// take.
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`

	// Page defaults to 1 and PageSize to 20, capped at 100.
	Page     int `json:"page,omitempty"`
	PageSize int `json:"page_size,omitempty"`
}

// Generate provisions a new wallet on the requested chain family. master
// wallets are root-of-trust; transit and static wallets attach to a master.
// Static wallets get a fixed deposit address per-customer (with optional
// callback_url for per-deposit webhooks).
//
// Label names the wallet for your own bookkeeping and works for every wallet
// type. Nothing set here is frozen at creation: rename the wallet later with
// [WalletsService.SetLabel], change its deposit webhook with
// [WalletsService.SetCallbackURL], and change the master it settles to with
// [WalletsService.RebindMaster].
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

// PayInHistory returns every pay-in that used one deposit address — the same
// records as [PayInsService.History], narrowed to a single wallet.
//
// Useful when a payer says they sent funds and you have the address but not the
// order: a deposit wallet can serve several orders over its lifetime, and this
// is the list of them.
//
// The rows are ordinary [PayIn] records and the page is the ordinary
// [HistoryMeta]. Only orders belonging to your project are returned, so an
// address you do not own yields an empty page rather than an error.
//
//	page, err := c.Wallets.PayInHistory(ctx, cryptochief.WalletPayInHistoryQuery{
//	    Address:  depositAddress,
//	    PageSize: 50,
//	})
func (s *WalletsService) PayInHistory(ctx context.Context, q WalletPayInHistoryQuery) (*PayInHistoryResponse, error) {
	var out PayInHistoryResponse
	if err := s.c.do(ctx, "/v1/wallets/history", q, &out); err != nil {
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

// RebindMaster re-points a transit or static wallet at another master wallet of
// the same project and returns the wallet as it stands afterwards.
//
// It moves no money. What changes is where the NEXT sweep settles — including
// sweeps already queued but not yet sent, which will land on the new master.
// Anything already swept stays on the previous master; move it with a payout if
// you need it elsewhere.
//
// The call is idempotent: re-binding a wallet to the master it is already bound
// to succeeds and changes nothing. Master wallets cannot be re-pointed, and the
// new master must belong to the same project and chain family and must not be
// frozen.
//
//	w, err := c.Wallets.RebindMaster(ctx, depositAddress, newMasterAddress)
//	// w.MasterWalletAddress is the master the next sweep will settle to.
func (s *WalletsService) RebindMaster(ctx context.Context, address, masterWalletAddress string) (*Wallet, error) {
	body := map[string]string{
		"address":               address,
		"master_wallet_address": masterWalletAddress,
	}
	var out Wallet
	if err := s.c.do(ctx, "/v1/wallets/rebind-master", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetCallbackURL sets or clears the deposit webhook of a static wallet after it
// has been created, and returns the wallet as it stands afterwards.
//
// Static wallets only — master and transit wallets have no deposit webhook and
// the endpoint refuses them with a 400.
//
// An empty callbackURL is a value, not an omission: it clears the webhook, and
// the SDK sends it on the wire as "" rather than leaving the field out. Use
// [WalletsService.ClearCallbackURL] to say so plainly.
//
// The new URL applies to deposits announced from here on. A deposit already
// announced is not re-announced to it.
func (s *WalletsService) SetCallbackURL(ctx context.Context, address, callbackURL string) (*Wallet, error) {
	// A map, not a struct with omitempty: "" must reach the platform as an
	// empty string — that is how a webhook is cleared.
	body := map[string]string{
		"address":      address,
		"callback_url": callbackURL,
	}
	var out Wallet
	if err := s.c.do(ctx, "/v1/wallets/callback-url", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClearCallbackURL removes the deposit webhook from a static wallet. It is
// [WalletsService.SetCallbackURL] with an empty URL, spelled out.
func (s *WalletsService) ClearCallbackURL(ctx context.Context, address string) (*Wallet, error) {
	return s.SetCallbackURL(ctx, address, "")
}

// SetLabel renames a wallet — or takes its name away — after it has been
// created, and returns the wallet as it stands afterwards.
//
// Every wallet type can be named: master, transit and static alike, unlike the
// deposit webhook, which is static-only. The label is your own bookkeeping name
// and carries no routing meaning. It is capped at 255 characters; a longer one
// is refused with LABEL_TOO_LONG.
//
// An empty label is a value, not an omission: it removes the name, and the SDK
// sends it on the wire as "" rather than leaving the field out. Use
// [WalletsService.ClearLabel] to say so plainly.
//
//	w, err := c.Wallets.SetLabel(ctx, depositAddress, "customer 4242")
//	// w.Label is the name the wallet now carries, empty if it has none.
func (s *WalletsService) SetLabel(ctx context.Context, address, label string) (*Wallet, error) {
	// A map, not a struct with omitempty: "" must reach the platform as an
	// empty string — that is how a name is removed.
	body := map[string]string{
		"address": address,
		"label":   label,
	}
	var out Wallet
	if err := s.c.do(ctx, "/v1/wallets/label", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClearLabel removes the name from a wallet. It is [WalletsService.SetLabel]
// with an empty label, spelled out.
func (s *WalletsService) ClearLabel(ctx context.Context, address string) (*Wallet, error) {
	return s.SetLabel(ctx, address, "")
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
