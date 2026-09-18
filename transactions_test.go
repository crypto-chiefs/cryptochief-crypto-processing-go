package cryptochief

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestTransactions_EstimateNativeWireShape asserts Transactions.Estimate posts
// the Sign body minus url_callback to /v1/transaction/estimate and maps every
// response field, including empty fiat strings.
func TestTransactions_EstimateNativeWireShape(t *testing.T) {
	const apiKey = "k"
	const addr = "0x000000000000000000000000000000000000dEaD"
	const addr2 = "0x1111111111111111111111111111111111111111"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, `{"network":"ETH_MAINNET","chain_family":"EVM","type":"native","from_address":"`+addr+`","to_address":"`+addr2+`","estimated_fee":"0.000021","estimated_fee_fiat":"0.08","required":"1.000021","required_fiat":"3901.08"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Transactions.Estimate(context.Background(), &EstimateTransactionRequest{
		Network:     ChainEthMainnet,
		FromAddress: addr,
		Type:        TxTypeNative,
		ToAddress:   addr2,
		Value:       "1000000000000000000",
	})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if sent.URLPath != "/v1/transaction/estimate" {
		t.Errorf("path: %q", sent.URLPath)
	}
	want := `{"from_address":"` + addr + `","network":"ETH_MAINNET","to_address":"` + addr2 + `","type":"native","value":"1000000000000000000"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if strings.Contains(string(sent.Body), "url_callback") {
		t.Errorf("estimate must not send url_callback, body = %s", sent.Body)
	}
	if err := verifyHMACv1(sent, apiKey, "/v1/transaction/estimate"); err != nil {
		t.Error(err)
	}
	if out.Network != ChainEthMainnet {
		t.Errorf("Network: %q", out.Network)
	}
	if out.ChainFamily != "EVM" {
		t.Errorf("ChainFamily: %q", out.ChainFamily)
	}
	if out.Type != TxTypeNative {
		t.Errorf("Type: %q", out.Type)
	}
	if out.FromAddress != addr || out.ToAddress != addr2 {
		t.Errorf("addresses: %q → %q", out.FromAddress, out.ToAddress)
	}
	if out.EstimatedFee != "0.000021" {
		t.Errorf("EstimatedFee: %q", out.EstimatedFee)
	}
	if out.EstimatedFeeFiat != "0.08" {
		t.Errorf("EstimatedFeeFiat: %q", out.EstimatedFeeFiat)
	}
	if out.Required != "1.000021" {
		t.Errorf("Required: %q", out.Required)
	}
	if out.RequiredFiat != "3901.08" {
		t.Errorf("RequiredFiat: %q", out.RequiredFiat)
	}
}

// TestTransactions_EstimateTokenWireShape: a token estimate carries the token
// contract, and fiat fields left empty by the API decode as "" — a missing
// rate is an annotation, not an error.
func TestTransactions_EstimateTokenWireShape(t *testing.T) {
	const addr = "0x000000000000000000000000000000000000dEaD"
	const addr2 = "0x1111111111111111111111111111111111111111"
	const usdt = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, `{"network":"ETH_MAINNET","chain_family":"EVM","type":"token","from_address":"`+addr+`","to_address":"`+addr2+`","estimated_fee":"0.000063","estimated_fee_fiat":"","required":"0.000063","required_fiat":""}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Transactions.Estimate(context.Background(), &EstimateTransactionRequest{
		Network:     ChainEthMainnet,
		FromAddress: addr,
		Type:        TxTypeToken,
		ToAddress:   addr2,
		Value:       "12500000",
		Contract:    usdt,
	})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	want := `{"contract":"` + usdt + `","from_address":"` + addr + `","network":"ETH_MAINNET","to_address":"` + addr2 + `","type":"token","value":"12500000"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if out.Type != TxTypeToken {
		t.Errorf("Type: %q", out.Type)
	}
	if out.EstimatedFee != "0.000063" {
		t.Errorf("EstimatedFee: %q", out.EstimatedFee)
	}
	// A token transfer locks only the fee in native coin, never the token value.
	if out.Required != "0.000063" {
		t.Errorf("Required: %q", out.Required)
	}
	if out.EstimatedFeeFiat != "" || out.RequiredFiat != "" {
		t.Errorf("fiat must stay empty when the rate is unavailable: %q/%q", out.EstimatedFeeFiat, out.RequiredFiat)
	}
	// The TRON-only breakdown stays off the wire for EVM.
	if out.FeeExpected != "" || out.FeeLimit != "" || out.Energy != 0 ||
		out.EnergyFee != "" || out.BandwidthFee != "" || out.ActivationFee != "" {
		t.Errorf("non-TRON estimate must have no TRON breakdown: %+v", out)
	}
}

// TestTransactions_EstimateTronFeeBreakdown: a TRON estimate carries the
// energy-pool-aware breakdown — fee_expected (net of the wallet's
// staked/delegated/rented pool), fee_limit (the on-chain cap), and the gross
// components that sum to estimated_fee.
func TestTransactions_EstimateTronFeeBreakdown(t *testing.T) {
	const from = "TNPee8f4rZQ7eHWvRzYFqhJmZ8zK5cEkLm"
	const to = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE"
	const usdt = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, `{"network":"TRON_MAINNET","chain_family":"TRON","type":"token","from_address":"`+from+`","to_address":"`+to+`","estimated_fee":"27.29892","estimated_fee_fiat":"8.12","required":"27.29892","required_fiat":"8.12","fee_expected":"0.345","fee_limit":"59.39892","energy":64285,"energy_fee":"26.95392","bandwidth_fee":"0.345","activation_fee":""}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Transactions.Estimate(context.Background(), &EstimateTransactionRequest{
		Network:     ChainTronMainnet,
		FromAddress: from,
		Type:        TxTypeToken,
		ToAddress:   to,
		Value:       "12500000",
		Contract:    usdt,
	})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if sent.URLPath != "/v1/transaction/estimate" {
		t.Errorf("path: %q", sent.URLPath)
	}
	if err := verifyHMACv1(sent, "k", "/v1/transaction/estimate"); err != nil {
		t.Error(err)
	}
	if out.EstimatedFee != "27.29892" {
		t.Errorf("EstimatedFee: %q", out.EstimatedFee)
	}
	if out.FeeExpected != "0.345" {
		t.Errorf("FeeExpected: %q", out.FeeExpected)
	}
	if out.FeeLimit != "59.39892" {
		t.Errorf("FeeLimit: %q", out.FeeLimit)
	}
	if out.Energy != 64285 {
		t.Errorf("Energy: %d", out.Energy)
	}
	if out.EnergyFee != "26.95392" || out.BandwidthFee != "0.345" {
		t.Errorf("fee components: energy %q, bandwidth %q", out.EnergyFee, out.BandwidthFee)
	}
	if out.ActivationFee != "" {
		t.Errorf("ActivationFee on a token transfer: %q", out.ActivationFee)
	}
}

// TestTransactions_EstimateTronActivationFee: a native TRX transfer to an
// address the chain has not seen yet carries the activation fee as the third
// gross component.
func TestTransactions_EstimateTronActivationFee(t *testing.T) {
	const from = "TNPee8f4rZQ7eHWvRzYFqhJmZ8zK5cEkLm"
	const to = "TBrandNewAddress1111111111111111"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"network":"TRON_MAINNET","chain_family":"TRON","type":"native","from_address":"`+from+`","to_address":"`+to+`","estimated_fee":"1.445","estimated_fee_fiat":"","required":"1.945","required_fiat":"","fee_expected":"1.445","energy_fee":"","bandwidth_fee":"0.345","activation_fee":"1.1"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Transactions.Estimate(context.Background(), &EstimateTransactionRequest{
		Network:     ChainTronMainnet,
		FromAddress: from,
		Type:        TxTypeNative,
		ToAddress:   to,
		Value:       "500000",
	})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if out.ActivationFee != "1.1" {
		t.Errorf("ActivationFee: %q", out.ActivationFee)
	}
	// A native transfer carries no fee_limit and burns no energy: both stay off
	// the wire.
	if out.FeeLimit != "" || out.Energy != 0 || out.EnergyFee != "" {
		t.Errorf("native estimate must have no fee_limit/energy: %+v", out)
	}
}

// TestTransactions_EstimateContractUnsupported: TxTypeContract has no
// fee-quote mode — the API's 400 reaches the caller as an *APIError with the
// stable code.
func TestTransactions_EstimateContractUnsupported(t *testing.T) {
	const addr = "0x000000000000000000000000000000000000dEaD"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"ok":false,"error":"CONTRACT_ESTIMATE_UNSUPPORTED","msg":"CONTRACT_ESTIMATE_UNSUPPORTED"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c.Transactions.Estimate(context.Background(), &EstimateTransactionRequest{
		Network:     ChainEthMainnet,
		FromAddress: addr,
		Type:        TxTypeContract,
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Code != CodeContractEstimateUnsupported {
		t.Errorf("Code = %q, want %q", apiErr.Code, CodeContractEstimateUnsupported)
	}
	if apiErr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want 400", apiErr.HTTPStatus)
	}
}
