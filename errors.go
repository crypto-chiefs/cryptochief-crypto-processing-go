package cryptochief

import (
	"errors"
	"fmt"
)

// APIError is the typed form of a Crypto Chief error response.
//
// A refusal arrives in one of three shapes. The gateway's own refusals put the
// machine code in "error" and an English sentence in "msg":
//
//	{"error":"LABEL_TOO_LONG","msg":"label is longer than 255 characters","ok":false}
//
// Refusals relayed from an upstream service mark "error" with the generic
// SERVICE_ERROR and carry the code in "msg":
//
//	{"error":"SERVICE_ERROR","msg":"wallet_not_found","ok":false}
//
// A white-label installation puts the code in error.details.code and the
// sentence in error.message:
//
//	{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"...","details":{"code":"SIGNATURE_REPLAYED"}}}
//
// A fourth shape is an order view answering a non-2xx from Energy.Rent or
// Native.Buy — the refused/unresolved order itself as the body. There "error"
// is the human reason and the machine code is "error_code"; those methods
// return the order instead of an error, so this shape reaches an APIError
// only through [Client.Request].
//
// All resolve onto Code, so Code is the stable string callers should switch
// on whichever shape the server used. Message keeps the human-readable text,
// and Raw keeps the whole body.
//
// Use [errors.Is] against the package-level sentinels below to test for a
// specific code:
//
//	if errors.Is(err, cryptochief.ErrInsufficientFunds) { ... }
//
// Or pull the code directly:
//
//	var apiErr *cryptochief.APIError
//	if errors.As(err, &apiErr) {
//	    switch apiErr.Code { ... }
//	}
type APIError struct {
	// HTTPStatus is the HTTP status code returned by the server.
	HTTPStatus int
	// Code is the stable string identifier callers should branch on.
	Code string
	// Message is a human-readable description (server's "msg" or "error" text).
	Message string
	// Raw is the raw response body for cases where the server returned
	// something the client could not classify.
	Raw []byte

	// serverTime is server_time of the response in Unix seconds, 0 if absent.
	serverTime int64
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.HTTPStatus == 0 {
		return fmt.Sprintf("cryptochief: %s", e.Code)
	}
	if e.Message != "" && e.Message != e.Code {
		return fmt.Sprintf("cryptochief: %d %s: %s", e.HTTPStatus, e.Code, e.Message)
	}
	return fmt.Sprintf("cryptochief: %d %s", e.HTTPStatus, e.Code)
}

// Is treats two APIErrors as equal when their Code matches. Lets callers
// write errors.Is(err, ErrInsufficientFunds) without worrying about pointer
// identity.
func (e *APIError) Is(target error) bool {
	t, ok := target.(*APIError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// Common error codes — exposed both as sentinel *APIError values (for
// errors.Is) and as plain string constants (for direct switch comparisons).
//
// This is not exhaustive — Crypto Chief defines more codes per endpoint, and
// the server is free to add new ones. Treat the Code field as opaque if you
// don't recognise it.
const (
	CodeInsufficientFunds    = "INSUFFICIENT_FUNDS"
	CodeInsufficientCredits  = "INSUFFICIENT_CREDITS"
	CodeDebtLimitExceeded    = "DEBT_LIMIT_EXCEEDED"
	CodeAssetNotEnabled      = "ASSET_NOT_ENABLED"
	CodeOrderAlreadyExists   = "ORDER_ALREADY_EXIST"
	CodeOrderCannotCancel    = "ORDER_CANNOT_CANCEL"
	CodeOrderNotLive         = "ORDER_NOT_LIVE"
	CodeAssetAlreadySelected = "ASSET_ALREADY_SELECTED"
	CodeInvalidParams        = "INVALID_PARAMS"
	// CodeLabelTooLong — a wallet label over 255 characters. Decided by the
	// gateway, which sends the code in "error" and the sentence in "msg";
	// like every gateway code it reaches the caller as Code.
	CodeLabelTooLong          = "LABEL_TOO_LONG"
	CodeServiceError          = "SERVICE_ERROR"
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeURLCallbackRequired   = "URL_CALLBACK_REQUIRED"
	CodeBatchEmpty            = "BATCH_EMPTY"
	CodeBatchTooLarge         = "BATCH_TOO_LARGE"
	CodeBatchDuplicateOrderID = "BATCH_DUPLICATE_ORDER_ID"
	CodeFromWalletNotOwned    = "FROM_WALLET_NOT_OWNED"
	CodeSignatureExpired      = "SIGNATURE_EXPIRED"
	CodeAlreadyExecuted       = "ALREADY_EXECUTED"
	CodePreflightFailed       = "PREFLIGHT_FAILED"
	CodeBroadcastFailed       = "BROADCAST_FAILED"
	CodeSignedTxMismatch      = "SIGNED_TX_MISMATCH"
	CodeContractRequired      = "CONTRACT_REQUIRED_FOR_TOKEN"
	CodeTransferFieldsForbid  = "TRANSFER_FIELDS_NOT_ALLOWED_FOR_CONTRACT"
	CodeCallsRequired         = "CALLS_REQUIRED"
	CodeCallsNotAllowed       = "CALLS_NOT_ALLOWED_FOR_TRANSFER"
	CodeContractCallsUnsupp   = "CONTRACT_CALLS_UNSUPPORTED_ON_NETWORK"
	// CodeNonceGap — EVM execute: a lower nonce of the address is held by
	// another signature that was not executed. Nothing was sent; the
	// transaction's ErrorReason names that signature when it is known. Execute
	// it first, then retry the same uuid.
	CodeNonceGap = "NONCE_GAP"
	// CodeNonceAlreadyUsed — EVM execute: the chain already used this
	// transaction's nonce. Nothing was sent by this call.
	CodeNonceAlreadyUsed = "NONCE_ALREADY_USED"
	// CodePreviousExecuteUnresolved — EVM sign: an earlier signature from the
	// same address has an execute whose outcome is not known yet. The code
	// may carry that signature's uuid
	// ("PREVIOUS_EXECUTE_UNRESOLVED: uuid=<uuid>"); compare with
	// strings.HasPrefix. Retry execute of that uuid instead of signing again.
	CodePreviousExecuteUnresolved = "PREVIOUS_EXECUTE_UNRESOLVED"
	// CodeContractEstimateUnsupported — Transactions.Estimate with
	// TxTypeContract: contract calls have no fee-quote mode.
	CodeContractEstimateUnsupported = "CONTRACT_ESTIMATE_UNSUPPORTED"
	// CodeIdempotencyKeyRequired — Energy.Rent without an Idempotency-Key on
	// the context (see WithIdempotencyKey): without the key a retry would buy
	// the energy a second time (HTTP 400).
	CodeIdempotencyKeyRequired = "IDEMPOTENCY_KEY_REQUIRED"
	CodeNetworkError           = "NETWORK_ERROR"

	// CodeNotFound — the object does not exist OR is not this project's; the two
	// are deliberately indistinguishable.
	CodeNotFound = "NOT_FOUND"

	// Webhook resend refusals (Client.Webhooks). All decided by the platform's
	// webhook service and relayed in the gateway's own envelope, so they reach
	// the caller as Code.
	//
	//   - CodeDeliverySuperseded — a newer event exists for the same object; only
	//     the latest event may be resent. Permanent.
	//   - CodeDeliveryInFlight — a worker holds the delivery, or it is already
	//     scheduled for an automatic retry.
	//   - CodeResendTooSoon — resent under a minute ago (HTTP 429, Retry-After).
	//   - CodeNoDeliveries — the static deposit never had a webhook queued: it
	//     arrived on a wallet with no callback_url.
	CodeDeliverySuperseded = "DELIVERY_SUPERSEDED"
	CodeDeliveryInFlight   = "DELIVERY_IN_FLIGHT"
	CodeResendTooSoon      = "RESEND_TOO_SOON"
	CodeNoDeliveries       = "NO_DELIVERIES"

	// Request refusals of the HMAC-SHA256 v1 signature and the body limit.
	//
	//   - CodeBadAuthHeaders — Merchant or an X-CC-* header is missing,
	//     repeated or malformed (HTTP 400).
	//   - CodeSignatureTimestampOutOfRange — X-CC-Timestamp is more than 300
	//     seconds from server time (HTTP 401, server_time in the body). The
	//     client sets its clock offset and repeats the request once.
	//   - CodeInvalidSignature — X-CC-Signature does not match (HTTP 401).
	//   - CodeSignatureReplayed — X-CC-Nonce was already used (HTTP 401).
	//   - CodePayloadTooLarge — the request body exceeds the limit (HTTP 413).
	CodeBadAuthHeaders               = "BAD_AUTH_HEADERS"
	CodeSignatureTimestampOutOfRange = "SIGNATURE_TIMESTAMP_OUT_OF_RANGE"
	CodeInvalidSignature             = "INVALID_SIGNATURE"
	CodeSignatureReplayed            = "SIGNATURE_REPLAYED"
	CodePayloadTooLarge              = "PAYLOAD_TOO_LARGE"
)

// Sentinel error values — use with errors.Is. (Pointer identity does not
// matter because APIError.Is compares Code.)
var (
	ErrInsufficientFunds     = &APIError{Code: CodeInsufficientFunds}
	ErrInsufficientCredits   = &APIError{Code: CodeInsufficientCredits}
	ErrDebtLimitExceeded     = &APIError{Code: CodeDebtLimitExceeded}
	ErrAssetNotEnabled       = &APIError{Code: CodeAssetNotEnabled}
	ErrOrderAlreadyExists    = &APIError{Code: CodeOrderAlreadyExists}
	ErrOrderCannotCancel     = &APIError{Code: CodeOrderCannotCancel}
	ErrOrderNotLive          = &APIError{Code: CodeOrderNotLive}
	ErrAssetAlreadySelected  = &APIError{Code: CodeAssetAlreadySelected}
	ErrInvalidParams         = &APIError{Code: CodeInvalidParams}
	ErrUnauthorized          = &APIError{Code: CodeUnauthorized}
	ErrBatchEmpty            = &APIError{Code: CodeBatchEmpty}
	ErrBatchTooLarge         = &APIError{Code: CodeBatchTooLarge}
	ErrBatchDuplicateOrderID = &APIError{Code: CodeBatchDuplicateOrderID}
	ErrFromWalletNotOwned    = &APIError{Code: CodeFromWalletNotOwned}
	ErrSignatureExpired      = &APIError{Code: CodeSignatureExpired}
	ErrAlreadyExecuted       = &APIError{Code: CodeAlreadyExecuted}
	ErrPreflightFailed       = &APIError{Code: CodePreflightFailed}
	ErrNonceGap              = &APIError{Code: CodeNonceGap}
	ErrNonceAlreadyUsed      = &APIError{Code: CodeNonceAlreadyUsed}

	ErrBadAuthHeaders               = &APIError{Code: CodeBadAuthHeaders}
	ErrSignatureTimestampOutOfRange = &APIError{Code: CodeSignatureTimestampOutOfRange}
	ErrSignatureReplayed            = &APIError{Code: CodeSignatureReplayed}
	ErrPayloadTooLarge              = &APIError{Code: CodePayloadTooLarge}
)

// IsRetryable reports whether an error is plausibly transient and worth
// retrying. The transport uses it internally; callers can use it too if they
// retry at a higher level.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		// Server gave us a structured response — only 5xx are retryable.
		// Don't retry validation/billing errors.
		return apiErr.HTTPStatus >= 500 || apiErr.Code == CodeNetworkError
	}
	// Bare network/transport errors — retryable.
	return true
}
