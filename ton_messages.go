package cryptochief

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TON internal message op codes from the public TIP standards.
const (
	tonOpJettonTransfer = 0x0f8a7ea5
	tonOpNFTTransfer    = 0x5fcc3d14
	tonOpTextComment    = 0x00000000
)

// buildJettonTransferBody serialises the standard Jetton "transfer" body
// (TIP-74 / TEP-74) into a BoC. Schema:
//
//	transfer#0f8a7ea5 query_id:uint64
//	  amount:(VarUInteger 16)
//	  destination:MsgAddress
//	  response_destination:MsgAddress
//	  custom_payload:(Maybe ^Cell)
//	  forward_ton_amount:(VarUInteger 16)
//	  forward_payload:(Either Cell ^Cell)
//	  = InternalMsgBody;
//
// destination is the recipient's main TON wallet (NOT their Jetton wallet —
// the network does the wallet-to-wallet hop on its own).
// responseDest typically equals the sender so the gas remainder comes back.
// forwardTON is the amount of TON that flows to destination's notify
// handler; 0 means "no notification", 1 nanoTON means "notify with zero
// gas budget" (useful for off-chain confirmation).
func buildJettonTransferBody(
	queryID uint64,
	amount *big.Int,
	destination, responseDest *address.Address,
	customPayload *cell.Cell,
	forwardTON *big.Int,
	forwardPayload *cell.Cell,
) ([]byte, error) {
	if amount == nil || amount.Sign() < 0 {
		return nil, fmt.Errorf("cryptochief/ton: jetton amount must be non-negative")
	}
	if forwardTON == nil {
		forwardTON = big.NewInt(0)
	}
	if destination == nil {
		return nil, fmt.Errorf("cryptochief/ton: destination required")
	}
	if responseDest == nil {
		responseDest = address.NewAddressNone()
	}

	b := cell.BeginCell().
		MustStoreUInt(tonOpJettonTransfer, 32).
		MustStoreUInt(queryID, 64).
		MustStoreBigCoins(amount).
		MustStoreAddr(destination).
		MustStoreAddr(responseDest).
		MustStoreMaybeRef(customPayload).
		MustStoreBigCoins(forwardTON)

	// forward_payload: Either Cell ^Cell — 1 bit prefix tells the receiver
	// whether the payload follows inline or as a reference. We use a ref
	// whenever the caller supplied a payload (keeps the parent cell small),
	// and the "inline & empty" form otherwise.
	if forwardPayload != nil {
		b = b.MustStoreBoolBit(true).MustStoreRef(forwardPayload)
	} else {
		b = b.MustStoreBoolBit(false)
	}
	return b.EndCell().ToBOC(), nil
}

// buildTextCommentCell builds a stand-alone cell containing the canonical
// text-comment payload (op 0 + UTF-8 Snake string). Used both as a top
// body (via buildTextCommentBody) and as a forward_payload ref inside a
// Jetton transfer when the caller supplied a Memo.
func buildTextCommentCell(text string) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(tonOpTextComment, 32).
		MustStoreStringSnake(text).
		EndCell()
}

// buildNFTTransferBody serialises the standard NFT "transfer" body
// (TIP-62 / TEP-62). Schema:
//
//	transfer#5fcc3d14 query_id:uint64
//	  new_owner:MsgAddress
//	  response_destination:MsgAddress
//	  custom_payload:(Maybe ^Cell)
//	  forward_amount:(VarUInteger 16)
//	  forward_payload:(Either Cell ^Cell)
//	  = InternalMsgBody;
func buildNFTTransferBody(
	queryID uint64,
	newOwner, responseDest *address.Address,
	customPayload *cell.Cell,
	forwardTON *big.Int,
	forwardPayload *cell.Cell,
) ([]byte, error) {
	if newOwner == nil {
		return nil, fmt.Errorf("cryptochief/ton: new owner required")
	}
	if forwardTON == nil {
		forwardTON = big.NewInt(0)
	}
	if responseDest == nil {
		responseDest = address.NewAddressNone()
	}
	b := cell.BeginCell().
		MustStoreUInt(tonOpNFTTransfer, 32).
		MustStoreUInt(queryID, 64).
		MustStoreAddr(newOwner).
		MustStoreAddr(responseDest).
		MustStoreMaybeRef(customPayload).
		MustStoreBigCoins(forwardTON)
	if forwardPayload != nil {
		b = b.MustStoreBoolBit(true).MustStoreRef(forwardPayload)
	} else {
		b = b.MustStoreBoolBit(false)
	}
	return b.EndCell().ToBOC(), nil
}

// buildTextCommentBody serialises a simple text comment (op=0x00000000 +
// UTF-8 string in Snake encoding). What wallets show in the transfer note.
func buildTextCommentBody(text string) ([]byte, error) {
	b := cell.BeginCell().
		MustStoreUInt(tonOpTextComment, 32).
		MustStoreStringSnake(text)
	return b.EndCell().ToBOC(), nil
}
