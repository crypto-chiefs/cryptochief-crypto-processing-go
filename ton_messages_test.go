package cryptochief

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TestJettonTransferBody — op code 0x0f8a7ea5 must be the first 32 bits of
// the resulting cell; query_id is the next 64 bits; amount comes after.
// Verifies by parsing the BoC back with tonutils-go (independent path —
// builder and parser are different code in that library).
func TestJettonTransferBody(t *testing.T) {
	dest := address.MustParseRawAddr("0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe")
	resp := address.MustParseAddr("EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs")
	amount := big.NewInt(12_500_000) // 12.5 USDT in base units

	bocBytes, err := buildJettonTransferBody(42, amount, dest, resp, nil, big.NewInt(1), nil)
	if err != nil {
		t.Fatal(err)
	}

	c, err := cell.FromBOC(bocBytes)
	if err != nil {
		t.Fatalf("FromBOC: %v", err)
	}
	sl := c.BeginParse()

	op, err := sl.LoadUInt(32)
	if err != nil {
		t.Fatal(err)
	}
	if op != tonOpJettonTransfer {
		t.Errorf("op: got %#x want %#x", op, tonOpJettonTransfer)
	}
	qid, _ := sl.LoadUInt(64)
	if qid != 42 {
		t.Errorf("query_id: %d", qid)
	}
	got, err := sl.LoadBigCoins()
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(amount) != 0 {
		t.Errorf("amount: got %s want %s", got, amount)
	}
	gotDest, err := sl.LoadAddr()
	if err != nil {
		t.Fatal(err)
	}
	if !gotDest.Equals(dest) {
		t.Errorf("destination: %s vs %s", gotDest, dest)
	}
}

// TestNFTTransferBody verifies op code 0x5fcc3d14 lands at the head of the cell.
func TestNFTTransferBody(t *testing.T) {
	newOwner := address.MustParseRawAddr("0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe")
	bocBytes, err := buildNFTTransferBody(0, newOwner, nil, nil, big.NewInt(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := cell.FromBOC(bocBytes)
	sl := c.BeginParse()
	op, _ := sl.LoadUInt(32)
	if op != tonOpNFTTransfer {
		t.Errorf("op: got %#x want %#x", op, tonOpNFTTransfer)
	}
}

// TestTextCommentBody round-trips a comment through builder → parser.
func TestTextCommentBody(t *testing.T) {
	bocBytes, err := buildTextCommentBody("hello world")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := cell.FromBOC(bocBytes)
	sl := c.BeginParse()
	op, _ := sl.LoadUInt(32)
	if op != tonOpTextComment {
		t.Errorf("op: got %#x want 0x00000000", op)
	}
	text, err := sl.LoadStringSnake()
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Errorf("text: %q", text)
	}
}

// TestJettonTransfer_EndToEnd drives the full helper against a stub
// server. Asserts that the signed call carries type=contract and that
// the BoC body decodes back as a Jetton transfer with the requested
// Memo embedded in forward_payload.
func TestJettonTransfer_EndToEnd(t *testing.T) {
	const resolvedWallet = "0:abc1230000000000000000000000000000000000000000000000000000000000"
	addrBOC := mustBuildAddrCellBOC(t, resolvedWallet)

	rpcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "runGetMethod"):
			_, _ = io.WriteString(w, `{"gas_used":1,"exit_code":0,"stack":[{"type":"slice","value":"`+addrBOC+`"}]}`)
		case strings.Contains(r.URL.Path, "jetton/wallets"):
			_, _ = io.WriteString(w, `{"jetton_wallets":[{"address":"`+resolvedWallet+`"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer rpcSrv.Close()

	var gotSignBody map[string]any
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotSignBody)
		_, _ = io.WriteString(w, `{"uuid":"u-1","status":"signed","signed_tx_hex":"","tx_hash":"hash"}`)
	}))
	defer gw.Close()

	c, err := New("m", "k",
		WithBaseURL(gw.URL),
		WithRetries(0),
		WithTONRPCBaseURL(rpcSrv.URL),
	)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := c.Transactions.JettonTransfer(context.Background(), &JettonTransferRequest{
		Network:      ChainTONMainnet,
		FromAddress:  "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs",
		JettonMaster: "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs",
		Recipient:    "0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe",
		Amount:       big.NewInt(1_000_000),
		Memo:         "Order #4242",
	})
	if err != nil {
		t.Fatal(err)
	}
	if signed.UUID != "u-1" {
		t.Errorf("uuid: %s", signed.UUID)
	}

	if gotSignBody["type"] != "contract" {
		t.Errorf("type: %v", gotSignBody["type"])
	}
	calls, _ := gotSignBody["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls: %v", gotSignBody["calls"])
	}
	call := calls[0].(map[string]any)
	dataB64, _ := call["data"].(string)
	if dataB64 == "" {
		t.Fatal("data missing")
	}
	rawBody, _ := base64.StdEncoding.DecodeString(dataB64)
	parsed, err := cell.FromBOC(rawBody)
	if err != nil {
		t.Fatalf("body parse: %v", err)
	}
	sl := parsed.BeginParse()
	op, _ := sl.LoadUInt(32)
	if op != tonOpJettonTransfer {
		t.Errorf("op in body: %#x", op)
	}
	_, _ = sl.LoadUInt(64)   // query_id
	_, _ = sl.LoadBigCoins() // jetton amount
	_, _ = sl.LoadAddr()     // destination
	_, _ = sl.LoadAddr()     // response_destination
	_, _ = sl.LoadMaybeRef() // custom_payload
	_, _ = sl.LoadBigCoins() // forward_ton_amount
	either, _ := sl.LoadBoolBit()
	if !either {
		t.Fatal("Memo should be stored as forward_payload ref")
	}
	fwdRef, err := sl.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	fwdOp, _ := fwdRef.LoadUInt(32)
	if fwdOp != tonOpTextComment {
		t.Errorf("forward_payload op: %#x want 0", fwdOp)
	}
	txt, _ := fwdRef.LoadStringSnake()
	if txt != "Order #4242" {
		t.Errorf("memo text: %q", txt)
	}
}

func mustBuildAddrCellBOC(t *testing.T, addrStr string) string {
	t.Helper()
	a, err := address.ParseRawAddr(addrStr)
	if err != nil {
		t.Fatal(err)
	}
	c := cell.BeginCell().MustStoreAddr(a).EndCell()
	return base64.StdEncoding.EncodeToString(c.ToBOC())
}

// TestJettonTransfer_AllExplicit_NoRPC — when JettonWalletAddress AND
// AttachedTON are both supplied, the helper must not touch the TON RPC
// at all (so callers that pre-resolve everything stay fully offline).
func TestJettonTransfer_AllExplicit_NoRPC(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"uuid":"u-2","status":"signed","signed_tx_hex":"","tx_hash":"h"}`)
	}))
	defer gw.Close()

	// Point the TON RPC at a server that fails every request — if the
	// helper touches it, the test fails.
	rpcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("RPC must not be called when JettonWalletAddress + AttachedTON are explicit; got %s", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer rpcSrv.Close()

	c, _ := New("m", "k",
		WithBaseURL(gw.URL),
		WithRetries(0),
		WithTONRPCBaseURL(rpcSrv.URL),
	)
	_, err := c.Transactions.JettonTransfer(context.Background(), &JettonTransferRequest{
		Network:             ChainTONMainnet,
		FromAddress:         "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs",
		JettonWalletAddress: "0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe",
		Recipient:           "0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe",
		Amount:              big.NewInt(1_000_000),
		AttachedTON:         NanoTON("0.05"),
	})
	if err != nil {
		t.Fatalf("all-explicit path must succeed without RPC: %v", err)
	}
}

func TestNanoTON(t *testing.T) {
	cases := map[string]string{
		"0":             "0",
		"0.5":           "500000000",
		"1":             "1000000000",
		"0.05":          "50000000",
		"123.456789012": "123456789012",
	}
	for in, want := range cases {
		if got := NanoTON(in); got != want {
			t.Errorf("NanoTON(%q): got %s want %s", in, got, want)
		}
	}
}
