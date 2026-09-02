package cryptochief

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

// /v1/blockchains/list answers with a BARE ARRAY, not the {"items":[…]} envelope
// every other list endpoint uses. A decoder written for the envelope compiles
// fine and fails only against the real platform, so the array is what the test
// feeds it.
func TestSupportedChains_BareArrayDecodes(t *testing.T) {
	var path, body string
	srv := captureServer(t, `[
		{"name":"ETH_MAINNET","type":"evm"},
		{"name":"TRON_MAINNET","type":"tron"},
		{"name":"SOLANA_MAINNET","type":"solana"}
	]`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	chains, err := c.Blockchain.SupportedChains(context.Background())
	if err != nil {
		t.Fatalf("SupportedChains: %v", err)
	}
	if path != "/v1/blockchains/list" {
		t.Errorf("path = %q", path)
	}
	// Nothing to filter by, but the empty object still has to be signed.
	if body != "{}" {
		t.Errorf("body = %q, want an empty object", body)
	}

	if len(chains) != 3 {
		t.Fatalf("chains = %d, want the whole array decoded", len(chains))
	}
	if chains[0].Name != ChainEthMainnet || chains[0].Type != "evm" {
		t.Errorf("chains[0] = %+v", chains[0])
	}
	if chains[1].Name != ChainTronMainnet || chains[1].Type != "tron" {
		t.Errorf("chains[1] = %+v", chains[1])
	}
	// The type is the scanner's lowercase protocol family, not the ChainFamily
	// spelling ("SOLANA") that responses elsewhere carry.
	if chains[2].Type != "solana" {
		t.Errorf("chains[2] type = %q, want the lowercase scanner family", chains[2].Type)
	}
}

// The platform catalogue carries chain_family and is_test, and a native coin's
// contract is an empty string rather than null. All three must survive
// decoding: dropping is_test silently mixes test assets into a mainnet picker.
func TestContractsList_ChainFamilyIsTestAndNativeContract(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"network":"ETH_MAINNET","coin":"ETH","contract":"","chain_family":"EVM","type":"native","is_test":false,"decimals":18},
		{"network":"TRON_MAINNET","coin":"USDT","contract":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t","chain_family":"TRON","type":"token","is_test":false,"decimals":6},
		{"network":"SOLANA_DEVNET","coin":"SOL","contract":"","chain_family":"SOLANA","type":"native","is_test":true,"decimals":9}
	]}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Blockchain.ContractsList(context.Background())
	if err != nil {
		t.Fatalf("ContractsList: %v", err)
	}
	if path != "/v1/blockchain/contracts/list" {
		t.Errorf("path = %q", path)
	}
	// The catalogue is platform-wide: there is nothing to filter by, and the
	// empty object still has to be signed.
	if body != "{}" {
		t.Errorf("body = %q, want an empty object", body)
	}
	if len(out.Items) != 3 {
		t.Fatalf("items = %d", len(out.Items))
	}

	native := out.Items[0]
	if native.ChainFamily != FamilyEVM {
		t.Errorf("chain_family = %q, want it decoded onto the item", native.ChainFamily)
	}
	if native.Contract != "" {
		t.Errorf("a native coin's contract = %q, want the empty string it was sent as", native.Contract)
	}
	if native.Type != "native" || native.Decimals != 18 || native.IsTest {
		t.Errorf("native item = %+v", native)
	}

	token := out.Items[1]
	if token.ChainFamily != FamilyTRON || token.Contract == "" || token.Decimals != 6 {
		t.Errorf("token item = %+v", token)
	}

	// is_test is the only thing separating a devnet asset from a real one.
	testnet := out.Items[2]
	if !testnet.IsTest {
		t.Errorf("devnet item = %+v, want is_test true", testnet)
	}
	if testnet.ChainFamily != FamilySolana || testnet.Contract != "" {
		t.Errorf("devnet item = %+v", testnet)
	}
}

// The project's own list sends the same item shape, so the two fields added for
// the catalogue must decode there too - a caller reading one type should not
// find it half-populated depending on which endpoint filled it.
func TestContractsAvailable_CarriesChainFamilyAndIsTest(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"network":"ETH_SEPOLIA","coin":"ETH","contract":"","chain_family":"EVM","type":"native","is_test":true,"decimals":18}
	]}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Blockchain.ContractsAvailable(context.Background(), ChainEthSepolia)
	if err != nil {
		t.Fatalf("ContractsAvailable: %v", err)
	}
	if path != "/v1/blockchain/contracts/available" {
		t.Errorf("path = %q", path)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d", len(out.Items))
	}
	if out.Items[0].ChainFamily != FamilyEVM || !out.Items[0].IsTest {
		t.Errorf("item = %+v, want chain_family and is_test decoded", out.Items[0])
	}
}

// The service builds this array from a Go slice, so an EMPTY result reaches the
// wire as a literal `null` rather than `[]`. A method whose signature promises a
// list must answer that with an empty list: no error, no nil slice, no decode
// failure.
func TestSupportedChains_NullBodyIsAnEmptyList(t *testing.T) {
	var path, body string
	srv := captureServer(t, `null`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	chains, err := c.Blockchain.SupportedChains(context.Background())
	if err != nil {
		t.Fatalf("SupportedChains on a null body: %v, want no error", err)
	}
	if chains == nil {
		t.Fatal("chains = nil, want an empty slice - a nil slice re-marshals as null and spreads the defect")
	}
	if len(chains) != 0 {
		t.Fatalf("chains = %v, want it empty", chains)
	}
	// The promise is a usable list, so it has to survive a round trip as [].
	out, err := json.Marshal(chains)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != "[]" {
		t.Errorf("re-marshalled = %s, want []", out)
	}
	for range chains { // must not panic
		t.Error("an empty list yielded an element")
	}
}

// is_test is the one field separating a test asset from a real one, so `false`
// has to survive a round trip through the struct. With omitempty it would not:
// the mainnet row would re-marshal without the field at all, and "mainnet asset"
// would read back as "the row says nothing".
func TestAvailableContract_IsTestFalseSurvivesRemarshalling(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"network":"ETH_MAINNET","coin":"ETH","contract":"","chain_family":"EVM","type":"native","is_test":false,"decimals":18}
	]}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Blockchain.ContractsList(context.Background())
	if err != nil {
		t.Fatalf("ContractsList: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d", len(out.Items))
	}

	encoded, err := json.Marshal(out.Items[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"is_test":false`)) {
		t.Errorf("re-marshalled mainnet row = %s, want it to still carry is_test:false", encoded)
	}

	// And the round trip must not turn a mainnet asset into an unknown one.
	var back AvailableContract
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.IsTest {
		t.Errorf("round-tripped item = %+v, want is_test still false", back)
	}
}
