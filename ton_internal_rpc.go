package cryptochief

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// tonRPC talks to the Crypto Chief TON RPC service. Kept unexported on
// purpose: this layer exists only to feed parameters (e.g. the sender's
// Jetton wallet address) into the high-level Sign* helpers so callers
// never touch RPC primitives directly.
type tonRPC struct {
	baseURL    string // e.g. https://rpc.crypto-chief.com
	merchantID string // path-segment credential — same Merchant ID used by the processing API
	httpClient *http.Client
	userAgent  string

	// jettonWalletCache deduplicates "owner|master" → wallet address lookups
	// across a long-lived Client.
	jettonWalletCache sync.Map // map[string]string
}

func newTONRPC(merchantID, baseURL string, hc *http.Client, ua string) *tonRPC {
	if baseURL == "" {
		baseURL = defaultTONRPCBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	if ua == "" {
		ua = fmt.Sprintf("cryptochief-go/%s", Version)
	}
	return &tonRPC{baseURL: baseURL, merchantID: merchantID, httpClient: hc, userAgent: ua}
}

const defaultTONRPCBaseURL = "https://rpc.crypto-chief.com"

// urlFor builds the absolute URL for one of the TON RPC endpoints. The
// {merchantID} segment is the same Merchant ID used on the rest of the
// Crypto Chief API.
func (r *tonRPC) urlFor(path string, q url.Values) string {
	u := fmt.Sprintf("%s/ton-v3/%s/%s", r.baseURL, r.merchantID, strings.TrimLeft(path, "/"))
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func (r *tonRPC) doGet(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.urlFor(path, q), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", r.userAgent)
	return r.do(req, out, path)
}

func (r *tonRPC) doPost(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cryptochief/ton: marshal %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.urlFor(path, nil), strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", r.userAgent)
	return r.do(req, out, path)
}

func (r *tonRPC) do(req *http.Request, out any, path string) error {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cryptochief/ton: %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return fmt.Errorf("cryptochief/ton: read %s: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("cryptochief/ton: %s: HTTP %d: %s", path, resp.StatusCode, truncate(raw, 256))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("cryptochief/ton: decode %s: %w", path, err)
	}
	return nil
}

// lookupJettonWallet returns the on-chain Jetton wallet address that holds
// `owner`'s balance of the Jetton minted by `jettonMaster`.
//
// Two-tier strategy:
//
//  1. Primary — call `get_wallet_address(slice owner)` on the Jetton master.
//     Deterministic; works even for an owner that has never received this
//     Jetton (the address is computed from a code+data hash, not from
//     on-chain state).
//  2. Fallback — query for the Jetton wallet by owner+master.
//
// Results are cached in-memory for the lifetime of the [Client].
func (r *tonRPC) lookupJettonWallet(ctx context.Context, jettonMaster, owner string) (string, error) {
	if jettonMaster == "" || owner == "" {
		return "", fmt.Errorf("cryptochief/ton: jettonMaster and owner are required")
	}
	cacheKey := owner + "|" + jettonMaster
	if v, ok := r.jettonWalletCache.Load(cacheKey); ok {
		return v.(string), nil
	}

	if addr, err := r.jettonWalletViaRunMethod(ctx, jettonMaster, owner); err == nil && addr != "" {
		r.jettonWalletCache.Store(cacheKey, addr)
		return addr, nil
	}

	addr, err := r.jettonWalletViaIndex(ctx, jettonMaster, owner)
	if err != nil {
		return "", err
	}
	r.jettonWalletCache.Store(cacheKey, addr)
	return addr, nil
}

// jettonWalletViaRunMethod calls get_wallet_address on the Jetton master.
// Per TEP-74 the stack input is one slice (the owner address as a
// MsgAddress) and the output stack[0] is a slice with the Jetton wallet
// address.
func (r *tonRPC) jettonWalletViaRunMethod(ctx context.Context, jettonMaster, owner string) (string, error) {
	ownerAddr, err := address.ParseAddr(owner)
	if err != nil {
		ownerAddr, err = address.ParseRawAddr(owner)
		if err != nil {
			return "", fmt.Errorf("parse owner: %w", err)
		}
	}
	ownerCell := cell.BeginCell().MustStoreAddr(ownerAddr).EndCell()
	ownerBoC := base64.StdEncoding.EncodeToString(ownerCell.ToBOCWithFlags(false))

	body := map[string]any{
		"address": jettonMaster,
		"method":  "get_wallet_address",
		"stack": []map[string]string{
			{"type": "slice", "value": ownerBoC},
		},
	}
	var out struct {
		GasUsed  int `json:"gas_used"`
		ExitCode int `json:"exit_code"`
		Stack    []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"stack"`
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := r.doPost(cctx, "/runGetMethod", body, &out); err != nil {
		return "", err
	}
	if out.ExitCode != 0 {
		return "", fmt.Errorf("get_wallet_address: exit_code=%d", out.ExitCode)
	}
	if len(out.Stack) == 0 {
		return "", errors.New("get_wallet_address: empty stack")
	}
	resultCell, err := cell.FromBOC(mustDecodeBase64(out.Stack[0].Value))
	if err != nil {
		return "", fmt.Errorf("decode result cell: %w", err)
	}
	slice := resultCell.BeginParse()
	resolved, err := slice.LoadAddr()
	if err != nil {
		return "", fmt.Errorf("load address from slice: %w", err)
	}
	return resolved.String(), nil
}

// jettonWalletViaIndex looks the Jetton wallet up by owner+master.
func (r *tonRPC) jettonWalletViaIndex(ctx context.Context, jettonMaster, owner string) (string, error) {
	q := url.Values{
		"owner_address":  []string{owner},
		"jetton_address": []string{jettonMaster},
		"limit":          []string{"1"},
	}
	var out struct {
		JettonWallets []struct {
			Address string `json:"address"`
		} `json:"jetton_wallets"`
		AddressBook map[string]struct {
			UserFriendly string `json:"user_friendly"`
		} `json:"address_book"`
	}
	if err := r.doGet(ctx, "/jetton/wallets", q, &out); err != nil {
		return "", err
	}
	if len(out.JettonWallets) == 0 {
		return "", fmt.Errorf("cryptochief/ton: no Jetton wallet found for owner %s on master %s — owner has never received this Jetton", owner, jettonMaster)
	}
	rawAddr := out.JettonWallets[0].Address
	// Prefer the user-friendly form when one is provided.
	if info, ok := out.AddressBook[rawAddr]; ok && info.UserFriendly != "" {
		return info.UserFriendly, nil
	}
	return rawAddr, nil
}

// hasJettonWallet reports whether `owner` already holds an initialised
// Jetton wallet for `jettonMaster`. Used by the high-level
// JettonTransfer helper to size the attached gas budget: a fresh
// receiver needs the sender's Jetton wallet to deploy a new account,
// which costs noticeably more gas.
func (r *tonRPC) hasJettonWallet(ctx context.Context, jettonMaster, owner string) bool {
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := url.Values{
		"owner_address":  []string{owner},
		"jetton_address": []string{jettonMaster},
		"limit":          []string{"1"},
	}
	var out struct {
		JettonWallets []json.RawMessage `json:"jetton_wallets"`
	}
	if err := r.doGet(qctx, "/jetton/wallets", q, &out); err != nil {
		return false
	}
	return len(out.JettonWallets) > 0
}

func mustDecodeBase64(s string) []byte {
	b, _ := base64.StdEncoding.DecodeString(s)
	return b
}
