package cryptochief

import "context"

// PayInsService groups the incoming-payment endpoints. Access via
// Client.PayIns.
type PayInsService struct{ c *Client }

// PayIn mode tells the API which amount to fix: FIAT lets the customer
// see a stable fiat price while crypto amounts vary by FX, CRYPTO fixes the
// crypto amount upfront.
type PayInMode string

const (
	PayInModeFiat   PayInMode = "fiat"
	PayInModeCrypto PayInMode = "crypto"
)

// PayIn status values surface in PayIn.Status.
const (
	PayInStatusWaitingAssetSelect = "waiting_asset_select"
	PayInStatusPending            = "pending"
	PayInStatusProcessing         = "processing"
	PayInStatusProcess            = "process"
	PayInStatusPaid               = "paid"
	PayInStatusCancel             = "cancel"
	PayInStatusExpired            = "expired"
)

// CreatePayInRequest is the body of POST /v1/payments/order/create.
// The two environments an order can belong to. A project may be allowed one or
// both; asking for testnet on a project that does not permit it is refused with
// TESTNET_NOT_ALLOWED rather than quietly served on mainnet, and a value that is
// neither is ENVIRONMENT_INVALID rather than a silent fallback.
const (
	EnvironmentMainnet = "mainnet"
	EnvironmentTestnet = "testnet"
)

type CreatePayInRequest struct {
	OrderID   string    `json:"order_id"`
	UserID    string    `json:"user_id"`
	Mode      PayInMode `json:"mode"`
	ToAddress string    `json:"to_address,omitempty"`
	// MasterWalletAddress pins the transit deposit wallet of THIS order to
	// the given master wallet of the project — the address the funds are
	// swept to. Requires the order's asset/network chain family to match the
	// master wallet's; a foreign or mismatched address is rejected with 400.
	// Omit for the previous project-default behavior.
	MasterWalletAddress string `json:"master_wallet_address,omitempty"`
	// Environment constrains the asset the platform PICKS for this order to
	// the real chains or the test ones: EnvironmentMainnet or
	// EnvironmentTestnet. Omit to use the project's own default.
	//
	// It changes nothing when Asset names a concrete network - that is the
	// caller's choice. It matters in fiat mode and when the network is ANY,
	// where the platform selects the asset and an unconstrained pick could put
	// a real payment on a test network.
	Environment string `json:"environment,omitempty"`

	LifetimeSec            int    `json:"lifetime_sec,omitempty"`
	URLCallback            string `json:"url_callback,omitempty"`
	URLSuccess             string `json:"url_success,omitempty"`
	URLError               string `json:"url_error,omitempty"`
	AdditionalData         string `json:"additional_data,omitempty"`
	AccuracyPaymentPercent int    `json:"accuracy_payment_percent,omitempty"`

	// FIAT-mode fields.
	AmountFiat   string `json:"amount_fiat,omitempty"`
	Currency     string `json:"currency,omitempty"`
	CourseSource string `json:"course_source,omitempty"` // e.g. "binance", "any"
	// Assets restricts which coins the customer may pick (allow / exclude
	// lists). Nil = offer every enabled asset.
	Assets *AssetsPolicy `json:"assets,omitempty"`

	// CRYPTO-mode fields.
	AmountCrypto string `json:"amount_crypto,omitempty"`
	// Asset fixes the exact coin+network the customer must pay with.
	Asset *Asset `json:"asset,omitempty"`
}

// CoinOption is one entry of the PayIn coin/network menu.
type CoinOption struct {
	ChainFamily ChainFamily `json:"chain_family"`
	Coin        string      `json:"coin"`
	Network     Chain       `json:"network"`
	Contract    string      `json:"contract,omitempty"`
}

// PayIn is the persistent record of one incoming order.
type PayIn struct {
	Type           string       `json:"type"`
	UUID           string       `json:"uuid"`
	OrderID        string       `json:"order_id"`
	UserID         string       `json:"user_id,omitempty"`
	Status         string       `json:"status"`
	Mode           PayInMode    `json:"mode,omitempty"`
	AmountCrypto   string       `json:"amount_crypto,omitempty"`
	AmountFiat     string       `json:"amount_fiat,omitempty"`
	Currency       string       `json:"currency,omitempty"`
	PaymentCoin    string       `json:"payment_coin,omitempty"`
	PaymentNetwork Chain        `json:"payment_network,omitempty"`
	ToAddress      string       `json:"to_address,omitempty"`
	Coins          []CoinOption `json:"coins,omitempty"`
	PaymentLink    string       `json:"payment_link,omitempty"`
	URLCallback    string       `json:"url_callback,omitempty"`
	URLSuccess     string       `json:"url_success,omitempty"`
	URLError       string       `json:"url_error,omitempty"`
	AdditionalData string       `json:"additional_data,omitempty"`
	CanCancel      *bool        `json:"can_cancel,omitempty"`
	ExpiredAt      string       `json:"expired_at,omitempty"`
	CreatedAt      string       `json:"created_at,omitempty"`
	UpdatedAt      string       `json:"updated_at,omitempty"`
}

// IsTerminal reports whether the order has reached a final state.
func (p PayIn) IsTerminal() bool {
	switch p.Status {
	case PayInStatusPaid, PayInStatusCancel, PayInStatusExpired:
		return true
	}
	return false
}

// Succeeded reports whether the customer paid.
func (p PayIn) Succeeded() bool { return p.Status == PayInStatusPaid }

// PayInHistoryResponse is the page of orders.
type PayInHistoryResponse struct {
	Items []PayIn     `json:"items"`
	Meta  HistoryMeta `json:"meta"`
}

// SelectAssetRequest commits the customer's coin+network choice on a
// waiting_asset_select order (FIAT-mode flow).
type SelectAssetRequest struct {
	UUID    string `json:"uuid"`
	Coin    string `json:"coin"`
	Network Chain  `json:"network"`
	// MasterWalletAddress pins the order's transit deposit wallet to the
	// given project master wallet; see CreatePayInRequest. A value here
	// overrides one supplied at order create.
	MasterWalletAddress string `json:"master_wallet_address,omitempty"`
}

// Create opens a new PayIn order. Use Mode=FIAT to let the customer pick the
// asset at payment time (recommended for fiat-priced storefronts), or
// Mode=CRYPTO to fix a specific asset+amount upfront.
func (s *PayInsService) Create(ctx context.Context, in *CreatePayInRequest) (*PayIn, error) {
	var out PayIn
	if err := s.c.do(ctx, "/v1/payments/order/create", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SelectAsset commits the customer's coin/network choice on a
// waiting_asset_select order, returning the address and final amount to pay.
func (s *PayInsService) SelectAsset(ctx context.Context, in *SelectAssetRequest) (*PayIn, error) {
	var out PayIn
	if err := s.c.do(ctx, "/v1/payments/asset/select", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetAsset reverts a pending order to waiting_asset_select so the customer
// can pick a different coin/network. H2H-only.
func (s *PayInsService) ResetAsset(ctx context.Context, uuid string) (*PayIn, error) {
	var out PayIn
	if err := s.c.do(ctx, "/v1/payments/asset/reset", map[string]string{"uuid": uuid}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Cancel cancels an open order. Allowed in process / waiting_asset_select /
// processing / pending; the response carries can_cancel=false thereafter.
func (s *PayInsService) Cancel(ctx context.Context, uuid string) (*PayIn, error) {
	var out PayIn
	if err := s.c.do(ctx, "/v1/payments/order/cancel", map[string]string{"uuid": uuid}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Info fetches the current state of one PayIn by uuid.
func (s *PayInsService) Info(ctx context.Context, uuid string) (*PayIn, error) {
	var out PayIn
	if err := s.c.do(ctx, "/v1/payments/order/info", map[string]string{"uuid": uuid}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// History returns a paged list of PayIns.
func (s *PayInsService) History(ctx context.Context, q HistoryQuery) (*PayInHistoryResponse, error) {
	var out PayInHistoryResponse
	if err := s.c.do(ctx, "/v1/payments/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
