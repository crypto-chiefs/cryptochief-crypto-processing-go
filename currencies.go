package cryptochief

import "context"

// CurrenciesService groups the fiat/crypto rate-calculator endpoints and the
// two catalogues of what can be quoted. These do NOT move funds — they're pure
// rate quotes used to size payouts and PayIn amounts. Access via
// Client.Currencies.
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

// FiatCurrency is one fiat the platform can price in — a code accepted as the
// fiat side of [CurrenciesService.FiatToCrypto] / [CurrenciesService.CryptoToFiat]
// and as [CreatePayInRequest.Currency] on a FIAT-mode pay-in.
type FiatCurrency struct {
	// Code is the ISO 4217 code, e.g. "SEK".
	Code string `json:"code"`
	// Name is the display name, e.g. "Swedish Krona".
	Name string `json:"name"`
}

// CryptoCurrencies is the response of /v1/currencies/cryptos: every crypto
// ticker the platform has a rate for, quoted against Quote.
//
// This is RATE availability, not PAYMENT availability. A ticker listed here is
// one the platform can put a price on; it says nothing about whether your
// project can take a deposit, sweep or pay out in it. That catalogue is
// [BlockchainService.ContractsAvailable], and building an asset picker from
// this type instead offers customers assets orders will refuse.
type CryptoCurrencies struct {
	// Tickers is the union of every exchange's list, deduplicated.
	Tickers []string `json:"tickers"`
	// ByExchange is the tickers each exchange carries, keyed by exchange name
	// ("binance", "bybit", "exmo", "kucoin", …). Which exchange a ticker comes
	// from is what [ConvertRequest.Provider] selects.
	ByExchange map[string][]string `json:"by_exchange"`
	// Quote is the asset the rates are quoted against — "USDT".
	Quote string `json:"quote"`
	// Count is how many tickers Tickers holds, as the platform counted them.
	Count int `json:"count"`
}

// Fiats lists every fiat currency the platform can price an order in and quote
// a rate against — the codes to populate a currency selector with, and the
// values [CurrenciesService.FiatToCrypto] and a FIAT-mode pay-in accept.
//
// The endpoint answers with a bare JSON array rather than an {"items": …}
// envelope, which is why this returns a slice.
//
// An empty result can arrive as a literal JSON null rather than []. That is not
// an error and never yields a nil slice here: the result is always a usable
// slice, empty when there is nothing to report, so re-marshalling it writes []
// and not null.
//
//	fiats, err := c.Currencies.Fiats(ctx)
//	for _, f := range fiats {
//	    fmt.Println(f.Code, f.Name) // SEK Swedish Krona
//	}
func (s *CurrenciesService) Fiats(ctx context.Context) ([]FiatCurrency, error) {
	var out []FiatCurrency
	if err := s.c.do(ctx, "/v1/currencies/fiats", struct{}{}, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return []FiatCurrency{}, nil
	}
	return out, nil
}

// Cryptos lists every crypto ticker the platform has a rate for, against USDT,
// and which exchange each one comes from.
//
// Rate availability only: a ticker here can be quoted, which does NOT mean the
// platform takes deposits, sweeps or payouts in it. For what your project can
// actually be paid in, use [BlockchainService.ContractsAvailable] — an asset
// picker built from this list offers assets orders will refuse.
//
// An empty result can arrive as a literal JSON null — the whole body, or
// by_exchange, or one exchange's list inside it. None of those is an error and
// none of them yields a nil collection here: Tickers, ByExchange and every list
// inside ByExchange are always usable, empty where there is nothing to report.
// So ByExchange can be assigned to as well as read, and re-marshalling the
// result writes [] and {} rather than null.
func (s *CurrenciesService) Cryptos(ctx context.Context) (*CryptoCurrencies, error) {
	var out CryptoCurrencies
	if err := s.c.do(ctx, "/v1/currencies/cryptos", struct{}{}, &out); err != nil {
		return nil, err
	}
	if out.Tickers == nil {
		out.Tickers = []string{}
	}
	if out.ByExchange == nil {
		out.ByExchange = map[string][]string{}
	}
	for exchange, tickers := range out.ByExchange {
		if tickers == nil {
			out.ByExchange[exchange] = []string{}
		}
	}
	return &out, nil
}
