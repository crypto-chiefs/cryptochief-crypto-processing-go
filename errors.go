package cryptochief

import (
	"errors"
	"fmt"
)

// APIError is the typed form of a Crypto Chief error response.
//
// A refusal arrives in one of two shapes. The gateway's own refusals put the
// machine code in "error" and an English sentence in "msg":
//
//	{"error":"LABEL_TOO_LONG","msg":"label is longer than 255 characters","ok":false}
//
// Refusals relayed from an upstream service mark "error" with the generic
// SERVICE_ERROR and carry the code in "msg":
//
//	{"error":"SERVICE_ERROR","msg":"wallet_not_found","ok":false}
//
// Both resolve onto Code, so Code is the stable string callers should switch
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
	CodeNetworkError          = "NETWORK_ERROR"

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
