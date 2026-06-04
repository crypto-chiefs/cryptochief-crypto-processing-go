package cryptochief

import "context"

// CurrenciesService groups the fiat/crypto rate-calculator endpoints.
// These do NOT move funds — they're pure rate quotes used to size payouts
// and PayIn amounts. Access via Client.Currencies.
type CurrenciesService struct{ c *Client }

// ConvertRequest is the body of both /v1/currencies/convert/* endpoints.
type ConvertRequest struct {
	// Provider is the exchange data source to quote against (optional;
	// server picks a default).
	Provider string `json:"provider,omitempty"`
	// From is the source ticker (FIAT code for fiat-to-crypto; crypto
	// symbol for crypto-to-fiat).
	From string `json:"from"`
	// To is the destination ticker.
	To string `json:"to"`
	// Amount is the human-readable amount of the From currency.
	Amount string `json:"amount"`
}

// ConvertResponse is the rate quote.
type ConvertResponse struct {
	AmountCrypto    float64 `json:"amount_crypto"`
	AmountFiat      float64 `json:"amount_fiat"`
	Crypto          string  `json:"crypto"`
	CryptoToUSDT    float64 `json:"crypto_to_usdt"`
	Exchange        string  `json:"exchange"`
	Fiat            string  `json:"fiat"`
	FiatToUSD       float64 `json:"fiat_to_usd"`
	TimestampCrypto int64   `json:"timestamp_crypto"`
	TimestampFiat   int64   `json:"timestamp_fiat"`
}

// FiatToCrypto quotes how much of `to` (crypto) the given amount of `from`
// (fiat) is worth right now.
func (s *CurrenciesService) FiatToCrypto(ctx context.Context, in *ConvertRequest) (*ConvertResponse, error) {
	var out ConvertResponse
	if err := s.c.do(ctx, "/v1/currencies/convert/fiat-crypto", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CryptoToFiat quotes how much of `to` (fiat) the given amount of `from`
// (crypto) is worth right now.
func (s *CurrenciesService) CryptoToFiat(ctx context.Context, in *ConvertRequest) (*ConvertResponse, error) {
	var out ConvertResponse
	if err := s.c.do(ctx, "/v1/currencies/convert/crypto-fiat", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
