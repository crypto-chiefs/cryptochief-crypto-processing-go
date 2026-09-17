package cryptochief

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
)

// discard drops a method's result and keeps its error.
func discard[T any](_ T, err error) error { return err }

// nullMembers lists the paths of object members whose value is null.
func nullMembers(t *testing.T, body []byte) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	var out []string
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if e == nil {
					out = append(out, path+"."+k)
					continue
				}
				walk(e, path+"."+k)
			}
		case []any:
			for _, e := range x {
				walk(e, path+"[]")
			}
		}
	}
	walk(v, "$")
	sort.Strings(out)
	return out
}

// jsonValue decodes a JSON document with numbers kept as written.
func jsonValue(t *testing.T, body []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	if dec.More() {
		t.Fatalf("trailing data after JSON: %s", body)
	}
	return v
}

// jsonMemberEqual reports whether the top-level member key of the JSON object
// body equals the JSON value want.
func jsonMemberEqual(t *testing.T, body, key, want string) bool {
	t.Helper()
	obj, ok := jsonValue(t, []byte(body)).(map[string]any)
	if !ok {
		return false
	}
	got, ok := obj[key]
	return ok && reflect.DeepEqual(got, jsonValue(t, []byte(want)))
}

// TestRequestBodies_V090 pins request bodies of methods with optional fields
// to the JSON values version 0.9.0 sent; member order is not compared. An
// optional field left unset is omitted, never sent as null, and the signature
// covers the bytes sent.
func TestRequestBodies_V090(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, `null`)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	ctx := context.Background()
	str := func(s string) *string { return &s }
	yes, no := true, false
	const (
		addr  = "0x000000000000000000000000000000000000dEaD"
		addr2 = "0x1111111111111111111111111111111111111111"
		sol   = "11111111111111111111111111111111"
		ton   = "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs"
		uuid  = "0b7c7f2e-7a51-4d3b-9d7e-0d5c6f1a2b3c"
	)

	cases := []struct {
		name string
		path string
		body string
		call func() error
	}{
		{
			"Wallets.List", "/v1/wallets/list", `{}`,
			func() error { return discard(c.Wallets.List(ctx)) },
		},
		{
			"Blockchain.ContractsAvailable/all networks", "/v1/blockchain/contracts/available", `{}`,
			func() error { return discard(c.Blockchain.ContractsAvailable(ctx, "")) },
		},
		{
			"Blockchain.WalletBalance/no contracts", "/v1/blockchain/wallet/balance",
			`{"addresses":["0x000000000000000000000000000000000000dEaD"],"chain":"ETH_MAINNET"}`,
			func() error { return discard(c.Blockchain.WalletBalance(ctx, ChainEthMainnet, []string{addr})) },
		},
		{
			"Credits.Topup/required", "/v1/credits/topup", `{"amount":"25","currency":"USDT"}`,
			func() error {
				return discard(c.Credits.Topup(ctx, CreditsTopupRequest{Amount: "25", Currency: "USDT"}))
			},
		},
		{
			"Credits.Topup/all", "/v1/credits/topup",
			`{"amount":"25.10","currency":"USDC","url_error":"https://m.example/err","url_success":"https://m.example/ok?a=1\u0026b=2"}`,
			func() error {
				return discard(c.Credits.Topup(ctx, CreditsTopupRequest{Amount: "25.10", Currency: "USDC", URLSuccess: "https://m.example/ok?a=1&b=2", URLError: "https://m.example/err"}))
			},
		},
		{
			"Currencies.FiatToCrypto/no provider", "/v1/currencies/convert/fiat-crypto", `{"amount":"100","from":"USD","to":"BTC"}`,
			func() error {
				return discard(c.Currencies.FiatToCrypto(ctx, &ConvertRequest{From: "USD", To: "BTC", Amount: "100"}))
			},
		},
		{
			"PayIns.Create/required", "/v1/payments/order/create", `{"mode":"fiat","order_id":"o-1","user_id":"u-1"}`,
			func() error {
				return discard(c.PayIns.Create(ctx, &CreatePayInRequest{OrderID: "o-1", UserID: "u-1", Mode: PayInModeFiat}))
			},
		},
		{
			"PayIns.Create/fiat all", "/v1/payments/order/create",
			`{"accuracy_payment_percent":2,"additional_data":"{\"k\":\"v\"}","amount_fiat":"10.00","assets":{"allow":[{"coin":"USDT","network":"ETH_MAINNET"},{"coin":"BTC"}],"exclude":[{"network":"TRON_MAINNET"}]},"course_source":"binance","currency":"EUR","environment":"testnet","lifetime_sec":3600,"master_wallet_address":"0x1111111111111111111111111111111111111111","mode":"fiat","order_id":"o-2","to_address":"0x000000000000000000000000000000000000dEaD","url_callback":"https://m.example/cb","url_error":"https://m.example/err","url_success":"https://m.example/ok","user_id":"u-2"}`,
			func() error {
				return discard(c.PayIns.Create(ctx, &CreatePayInRequest{
					OrderID: "o-2", UserID: "u-2", Mode: PayInModeFiat, ToAddress: addr,
					MasterWalletAddress: addr2, Environment: EnvironmentTestnet, LifetimeSec: 3600,
					URLCallback: "https://m.example/cb", URLSuccess: "https://m.example/ok", URLError: "https://m.example/err",
					AdditionalData: `{"k":"v"}`, AccuracyPaymentPercent: 2,
					AmountFiat: "10.00", Currency: "EUR", CourseSource: "binance",
					Assets: &AssetsPolicy{
						Allow:   []Asset{{Network: ChainEthMainnet, Coin: "USDT"}, {Coin: "BTC"}},
						Exclude: []Asset{{Network: ChainTronMainnet}},
					},
				}))
			},
		},
		{
			"PayIns.Create/crypto", "/v1/payments/order/create",
			`{"amount_crypto":"0.00000001","asset":{"coin":"ETH","network":"ETH_MAINNET"},"environment":"mainnet","mode":"crypto","order_id":"o-3","user_id":"u-3"}`,
			func() error {
				return discard(c.PayIns.Create(ctx, &CreatePayInRequest{
					OrderID: "o-3", UserID: "u-3", Mode: PayInModeCrypto, AmountCrypto: "0.00000001",
					Asset: &Asset{Network: ChainEthMainnet, Coin: "ETH"}, Environment: EnvironmentMainnet,
				}))
			},
		},
		{
			"PayIns.Create/empty policy and asset", "/v1/payments/order/create",
			`{"asset":{},"assets":{},"mode":"fiat","order_id":"o-4","user_id":"u-4"}`,
			func() error {
				return discard(c.PayIns.Create(ctx, &CreatePayInRequest{
					OrderID: "o-4", UserID: "u-4", Mode: PayInModeFiat,
					Assets: &AssetsPolicy{Allow: []Asset{}}, Asset: &Asset{},
				}))
			},
		},
		{
			"PayIns.SelectAsset/required", "/v1/payments/asset/select",
			`{"coin":"USDT","network":"TRON_MAINNET","uuid":"0b7c7f2e-7a51-4d3b-9d7e-0d5c6f1a2b3c"}`,
			func() error {
				return discard(c.PayIns.SelectAsset(ctx, &SelectAssetRequest{UUID: uuid, Coin: "USDT", Network: ChainTronMainnet}))
			},
		},
		{
			"PayIns.History/zero", "/v1/payments/history", `{}`,
			func() error { return discard(c.PayIns.History(ctx, HistoryQuery{})) },
		},
		{
			"Payouts.Estimate/required", "/v1/payout/estimate",
			`{"amount":"0.5","coin":"ETH","network":"ETH_MAINNET","to_address":"0x000000000000000000000000000000000000dEaD"}`,
			func() error {
				return discard(c.Payouts.Estimate(ctx, &EstimatePayoutRequest{Network: ChainEthMainnet, Coin: "ETH", Amount: "0.5", ToAddress: addr}))
			},
		},
		{
			"Payouts.Estimate/empty policy and sources", "/v1/payout/estimate",
			`{"amount":"0.5","auto_convert_policy":{},"coin":"ETH","network":"ETH_MAINNET","to_address":"0x000000000000000000000000000000000000dEaD"}`,
			func() error {
				return discard(c.Payouts.Estimate(ctx, &EstimatePayoutRequest{Network: ChainEthMainnet, Coin: "ETH", Amount: "0.5", ToAddress: addr, FromAddresses: []string{}, AutoConvertPolicy: &AssetsPolicy{}}))
			},
		},
		{
			"Payouts.Execute/required", "/v1/payout/execute",
			`{"amount":"0.5","coin":"ETH","network":"ETH_MAINNET","order_id":"po-1","to_address":"0x000000000000000000000000000000000000dEaD","url_callback":"","user_id":"u-1"}`,
			func() error {
				return discard(c.Payouts.Execute(ctx, &ExecutePayoutRequest{OrderID: "po-1", UserID: "u-1", Network: ChainEthMainnet, Coin: "ETH", Amount: "0.5", ToAddress: addr}))
			},
		},
		{
			"Payouts.BatchExecute/no callback", "/v1/payout/batch/execute",
			`{"items":[{"amount":"1","coin":"ETH","network":"ETH_MAINNET","order_id":"b-1","to_address":"0x000000000000000000000000000000000000dEaD","url_callback":"","user_id":"u"}]}`,
			func() error {
				return discard(c.Payouts.BatchExecute(ctx, &BatchExecuteRequest{Items: []ExecutePayoutRequest{{OrderID: "b-1", UserID: "u", Network: ChainEthMainnet, Coin: "ETH", Amount: "1", ToAddress: addr}}}))
			},
		},
		{
			"StaticDeposits.History/zero", "/v1/static-deposit/history", `{}`,
			func() error { return discard(c.StaticDeposits.History(ctx, StaticDepositHistoryQuery{})) },
		},
		{
			"Sweeps.WalletHistory/required", "/v1/sweeps/wallet/history", `{"address":"0x000000000000000000000000000000000000dEaD"}`,
			func() error { return discard(c.Sweeps.WalletHistory(ctx, SweepWalletHistoryQuery{Address: addr})) },
		},
		{
			"Sweeps.Settings/project default", "/v1/sweeps/settings", `{}`,
			func() error { return discard(c.Sweeps.Settings(ctx, SweepSettingsQuery{})) },
		},
		{
			"Sweeps.UpdateSettings/required", "/v1/sweeps/settings/update", `{"address":"0x000000000000000000000000000000000000dEaD"}`,
			func() error { return discard(c.Sweeps.UpdateSettings(ctx, SweepSettingsUpdate{Address: addr})) },
		},
		{
			"Sweeps.UpdateSettings/all", "/v1/sweeps/settings/update",
			`{"address":"0x000000000000000000000000000000000000dEaD","fee_mode":"mix","fields":["type_work","threshold_amount_usd","fee_mode","gas_source"],"gas_source":"native","network_code":"TRON_MAINNET","threshold_amount_usd":"50.00","type_work":"threshold"}`,
			func() error {
				return discard(c.Sweeps.UpdateSettings(ctx, SweepSettingsUpdate{
					Address: addr, NetworkCode: ChainTronMainnet,
					Fields:   []string{"type_work", "threshold_amount_usd", "fee_mode", "gas_source"},
					TypeWork: str(SweepModeThreshold), ThresholdUSD: str("50.00"), FeeMode: str(SweepFeeModeMix), GasSource: str(SweepGasSourceNative),
				}))
			},
		},
		{
			"Sweeps.UpdateSettings/value without fields", "/v1/sweeps/settings/update",
			`{"address":"0x000000000000000000000000000000000000dEaD","type_work":"momentum"}`,
			func() error {
				return discard(c.Sweeps.UpdateSettings(ctx, SweepSettingsUpdate{Address: addr, TypeWork: str(SweepModeMomentum)}))
			},
		},
		{
			"Sweeps.UpdateSettings/drop overrides", "/v1/sweeps/settings/update",
			`{"address":"0x000000000000000000000000000000000000dEaD","fields":["gas_source","threshold_amount_usd"],"network_code":"TRON_MAINNET"}`,
			func() error {
				return discard(c.Sweeps.UpdateSettings(ctx, SweepSettingsUpdate{Address: addr, NetworkCode: ChainTronMainnet, Fields: []string{"gas_source", "threshold_amount_usd"}}))
			},
		},
		{
			"Sweeps.UpdateSettings/drop one set another", "/v1/sweeps/settings/update",
			`{"address":"0x000000000000000000000000000000000000dEaD","fields":["fee_mode","type_work"],"type_work":"turned_off"}`,
			func() error {
				return discard(c.Sweeps.UpdateSettings(ctx, SweepSettingsUpdate{Address: addr, Fields: []string{"fee_mode", "type_work"}, TypeWork: str(SweepModeOff)}))
			},
		},
		{
			"Sweeps.UpdateSettings/empty strings", "/v1/sweeps/settings/update",
			`{"address":"0x000000000000000000000000000000000000dEaD","fee_mode":"","gas_source":"","threshold_amount_usd":"","type_work":""}`,
			func() error {
				return discard(c.Sweeps.UpdateSettings(ctx, SweepSettingsUpdate{Address: addr, Fields: []string{}, TypeWork: str(""), ThresholdUSD: str(""), FeeMode: str(""), GasSource: str("")}))
			},
		},
		{
			"Transactions.Sign/native", "/v1/transaction/signature",
			`{"from_address":"0x000000000000000000000000000000000000dEaD","network":"ETH_MAINNET","to_address":"0x1111111111111111111111111111111111111111","type":"native","value":"1000"}`,
			func() error {
				return discard(c.Transactions.Sign(ctx, &SignTransactionRequest{Network: ChainEthMainnet, FromAddress: addr, Type: TxTypeNative, ToAddress: addr2, Value: "1000"}))
			},
		},
		{
			"Transactions.Sign/calls", "/v1/transaction/signature",
			`{"calls":[{"accounts":[{"is_signer":true,"is_writable":false,"pubkey":"11111111111111111111111111111111"},{"is_signer":false,"is_writable":false,"pubkey":"11111111111111111111111111111111"}],"data":"AQID","to":"11111111111111111111111111111111"},{"bounce":false,"data":"","to":"EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs","value":"0"},{"bounce":true,"data":"x","to":"EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs","value":"1"}],"from_address":"11111111111111111111111111111111","network":"SOLANA_DEVNET","type":"contract"}`,
			func() error {
				return discard(c.Transactions.Sign(ctx, &SignTransactionRequest{Network: ChainSolanaDevnet, FromAddress: sol, Type: TxTypeContract, Calls: []ContractCall{
					{To: sol, Data: "AQID", Accounts: []SolanaAccount{{Pubkey: sol, IsSigner: true}, {Pubkey: sol}}},
					{To: ton, Value: "0", Bounce: &no},
					{To: ton, Value: "1", Data: "x", Bounce: &yes, Accounts: []SolanaAccount{}},
				}}))
			},
		},
		{
			"Transactions.Execute/required", "/v1/transaction/execute", `{"uuid":"0b7c7f2e-7a51-4d3b-9d7e-0d5c6f1a2b3c"}`,
			func() error { return discard(c.Transactions.Execute(ctx, &ExecuteTransactionRequest{UUID: uuid})) },
		},
		{
			"Transactions.SignTONCall/no bounce", "/v1/transaction/signature",
			`{"calls":[{"data":"te4=","to":"EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs","value":"0"}],"from_address":"EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs","network":"TON_MAINNET","type":"contract"}`,
			func() error {
				return discard(c.Transactions.SignTONCall(ctx, &TONCallRequest{Network: ChainTONMainnet, FromAddress: ton, Contract: ton, BodyCell: []byte{0xb5, 0xee}}))
			},
		},
		{
			"Wallets.Generate/required", "/v1/wallets/generate", `{"chain_family":"EVM","wallet_type":"master"}`,
			func() error {
				return discard(c.Wallets.Generate(ctx, &GenerateWalletRequest{WalletType: WalletTypeMaster, ChainFamily: FamilyEVM}))
			},
		},
		{
			"Wallets.PayInHistory/required", "/v1/wallets/history", `{"address":"0x000000000000000000000000000000000000dEaD"}`,
			func() error { return discard(c.Wallets.PayInHistory(ctx, WalletPayInHistoryQuery{Address: addr})) },
		},
		{
			"Wallets.ClearCallbackURL", "/v1/wallets/callback-url", `{"address":"0x000000000000000000000000000000000000dEaD","callback_url":""}`,
			func() error { return discard(c.Wallets.ClearCallbackURL(ctx, addr)) },
		},
		{
			"Wallets.ClearLabel", "/v1/wallets/label", `{"address":"0x000000000000000000000000000000000000dEaD","label":""}`,
			func() error { return discard(c.Wallets.ClearLabel(ctx, addr)) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(rec.all())
			if err := tc.call(); err != nil {
				t.Fatalf("call: %v", err)
			}
			reqs := rec.all()[before:]
			if len(reqs) != 1 {
				t.Fatalf("requests: %d", len(reqs))
			}
			s := reqs[0]
			if s.URLPath != tc.path {
				t.Errorf("path = %q, want %q", s.URLPath, tc.path)
			}
			got, want := nullMembers(t, s.Body), nullMembers(t, []byte(tc.body))
			if len(got) != len(want) {
				t.Errorf("null members = %v, want %v", got, want)
			}
			if !reflect.DeepEqual(jsonValue(t, s.Body), jsonValue(t, []byte(tc.body))) {
				t.Errorf("body\n got %s\nwant %s", s.Body, tc.body)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, s.Body); err != nil || !bytes.Equal(compact.Bytes(), s.Body) {
				t.Errorf("body is not compact JSON: %s", s.Body)
			}
			if err := verifyHMACv1(s, hmacTestAPIKey, tc.path); err != nil {
				t.Error(err)
			}
		})
	}
}
