package cryptochief

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

// selHex is a tiny test helper — EVMSelector returns a [4]byte value, which
// can't be sliced inline, so we copy into a slice here.
func selHex(sig string) string {
	s := EVMSelector(sig)
	return hex.EncodeToString(s[:])
}

// TestEVMSelector checks the canonical-signature → 4-byte selector path
// against well-known function selectors from Ethereum's ecosystem.
func TestEVMSelector(t *testing.T) {
	cases := []struct {
		sig  string
		want string
	}{
		{"transfer(address,uint256)", "a9059cbb"}, // ERC-20
		{"approve(address,uint256)", "095ea7b3"},  // ERC-20
		{"balanceOf(address)", "70a08231"},        // ERC-20
		{"totalSupply()", "18160ddd"},             // ERC-20
		{"transferFrom(address,address,uint256)", "23b872dd"},
		// Uniswap V2 router — canonical selector from Etherscan.
		{"swapExactTokensForTokens(uint256,uint256,address[],address,uint256)", "38ed1739"},
		// Aliases — "uint" must canonicalise to "uint256".
		{"transfer(address,uint)", "a9059cbb"},
		// Spaces and named params must be stripped before hashing.
		{"transfer(address to, uint256 amount)", "a9059cbb"},
	}
	for _, tc := range cases {
		got := EVMSelector(tc.sig)
		if hex.EncodeToString(got[:]) != tc.want {
			t.Errorf("selector for %q: got %x want %s", tc.sig, got, tc.want)
		}
	}
}

// TestEncodeEVMCall_ERC20Transfer is the textbook ABI test vector — every
// Ethereum SDK ships this and produces identical bytes.
func TestEncodeEVMCall_ERC20Transfer(t *testing.T) {
	data, err := EncodeEVMCall("transfer(address,uint256)",
		"0xbcd4042de499d14e55001ccbb24a551f3b954096",
		big.NewInt(1_000_000),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "a9059cbb" + // selector
		"000000000000000000000000bcd4042de499d14e55001ccbb24a551f3b954096" + // recipient
		"00000000000000000000000000000000000000000000000000000000000f4240" //   amount
	if hex.EncodeToString(data) != want {
		t.Fatalf("\n got: %s\nwant: %s", hex.EncodeToString(data), want)
	}
}

// TestEncodeEVMCall_DynamicArray exercises head/tail packing for a dynamic
// array argument: address[] passed as the third (and dynamic) argument.
func TestEncodeEVMCall_DynamicArray(t *testing.T) {
	data, err := EncodeEVMCall("multiSend(address[],uint256[])",
		[]string{
			"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		[]*big.Int{big.NewInt(100), big.NewInt(200)},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Selector + two heads (each offset to its tail) + two tails (each length + items).
	// head1 = 0x40 (64 bytes — two heads of 32)
	// head2 = 0xa0 (offset to start of tail2 = head1 offset + tail1 length [32+32*2=96])
	want := strings.Join([]string{
		selHex("multiSend(address[],uint256[])"),
		"0000000000000000000000000000000000000000000000000000000000000040", // head1
		"00000000000000000000000000000000000000000000000000000000000000a0", // head2
		"0000000000000000000000000000000000000000000000000000000000000002", // tail1.len
		"000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"0000000000000000000000000000000000000000000000000000000000000002", // tail2.len
		"0000000000000000000000000000000000000000000000000000000000000064", // 100
		"00000000000000000000000000000000000000000000000000000000000000c8", // 200
	}, "")
	if hex.EncodeToString(data) != want {
		t.Fatalf("\n got: %s\nwant: %s", hex.EncodeToString(data), want)
	}
}

// TestEncodeEVMCall_DynamicBytesAndString verifies that two dynamic args are
// each given their own offset and their tails are 32-byte-padded.
func TestEncodeEVMCall_DynamicBytesAndString(t *testing.T) {
	data, err := EncodeEVMCall("bar(bytes,string)", "0xdeadbeef", "hello")
	if err != nil {
		t.Fatal(err)
	}
	// head1 = 0x40, head2 = 0x80 (after head1=64 + tail1=32+32=64 → next at 128)
	want := strings.Join([]string{
		selHex("bar(bytes,string)"),
		"0000000000000000000000000000000000000000000000000000000000000040", // head1
		"0000000000000000000000000000000000000000000000000000000000000080", // head2
		"0000000000000000000000000000000000000000000000000000000000000004", // tail1.len
		"deadbeef00000000000000000000000000000000000000000000000000000000", // tail1.data padded
		"0000000000000000000000000000000000000000000000000000000000000005", // tail2.len
		"68656c6c6f000000000000000000000000000000000000000000000000000000", // "hello" padded
	}, "")
	if hex.EncodeToString(data) != want {
		t.Fatalf("\n got: %s\nwant: %s", hex.EncodeToString(data), want)
	}
}

// TestEncodeEVMCall_AcceptsTronBase58 makes sure the TRON address shorthand
// works inside an EVM-ABI argument list — encode strips the 0x41 prefix and
// keeps the 20-byte form, matching what TRON contracts expect inside their
// own ABI parameters.
func TestEncodeEVMCall_AcceptsTronBase58(t *testing.T) {
	const usdtTron = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" // USDT TRC-20 contract
	const evmEquiv = "a614f803b6fd780986a42c78ec9c7f77e6ded13c"
	data, err := EncodeEVMCall("balanceOf(address)", usdtTron)
	if err != nil {
		t.Fatal(err)
	}
	want := EVMSelector("balanceOf(address)")
	got := data[:4]
	if !bytes.Equal(got, want[:]) {
		t.Errorf("selector: %x != %x", got, want)
	}
	// Slot is left-padded — 12 zero bytes + 20-byte address.
	addrSlot := data[4:36]
	if hex.EncodeToString(addrSlot[12:]) != evmEquiv {
		t.Fatalf("address slot: %s, want trailing %s", hex.EncodeToString(addrSlot), evmEquiv)
	}
	for i := 0; i < 12; i++ {
		if addrSlot[i] != 0 {
			t.Errorf("address slot not left-padded: %x", addrSlot)
		}
	}
}

// TestEncodeEVMCall_BoolAndBytes32_LengthMismatch — bytesN is fixed-width
// and must reject inputs of the wrong length rather than silently padding.
func TestEncodeEVMCall_BoolAndBytes32_LengthMismatch(t *testing.T) {
	_, err := EncodeEVMCall("twiddle(bool,bytes32)", true, "0xdeadbeef")
	if err == nil {
		t.Fatal("expected length error for short bytes32")
	}
	if !strings.Contains(err.Error(), "expected 32 bytes") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestEncodeEVMCall_Bytes32Padded uses the proper 32-byte input.
func TestEncodeEVMCall_Bytes32Padded(t *testing.T) {
	full := strings.Repeat("ab", 32)
	data, err := EncodeEVMCall("twiddle(bool,bytes32)", true, "0x"+full)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		selHex("twiddle(bool,bytes32)"),
		"0000000000000000000000000000000000000000000000000000000000000001",
		full,
	}, "")
	if hex.EncodeToString(data) != want {
		t.Fatalf("\n got: %s\nwant: %s", hex.EncodeToString(data), want)
	}
}

// TestEncodeEVMCall_ArgCountMismatch is a guardrail: the helper should refuse
// to silently encode the wrong number of arguments.
func TestEncodeEVMCall_ArgCountMismatch(t *testing.T) {
	_, err := EncodeEVMCall("transfer(address,uint256)", "0x0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error on arg-count mismatch")
	}
}

// TestEncodeEVMCall_AliasesAndWhitespace verifies parameter-name shorthand and
// "uint" → "uint256" expansion both compile to the textbook ERC-20 bytes.
func TestEncodeEVMCall_AliasesAndWhitespace(t *testing.T) {
	want := "a9059cbb" +
		"000000000000000000000000bcd4042de499d14e55001ccbb24a551f3b954096" +
		"00000000000000000000000000000000000000000000000000000000000f4240"
	cases := []string{
		"transfer(address,uint256)",
		"transfer(address,uint)",
		"transfer(address to, uint256 amount)",
		"transfer ( address to , uint amount )",
	}
	for _, sig := range cases {
		data, err := EncodeEVMCall(sig,
			"0xbcd4042de499d14e55001ccbb24a551f3b954096",
			big.NewInt(1_000_000))
		if err != nil {
			t.Errorf("%q: %v", sig, err)
			continue
		}
		if hex.EncodeToString(data) != want {
			t.Errorf("%q:\n got %s\nwant %s", sig, hex.EncodeToString(data), want)
		}
	}
}
