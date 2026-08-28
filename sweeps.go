package cryptochief

import "context"

// SweepsService groups treasury sweep endpoints. A sweep moves funds from a
// transit wallet to the project's master wallet. Access via Client.Sweeps.
type SweepsService struct{ c *Client }

// SweepMode filters Sweeps.History by trigger source.
type SweepMode string

const (
	SweepModeAuto  SweepMode = "auto"
	SweepModeForce SweepMode = "force"
)

// SweepHistoryQuery is the body of /v1/sweeps/history.
type SweepHistoryQuery struct {
	Mode     SweepMode `json:"mode,omitempty"`
	Page     int       `json:"page,omitempty"`
	PageSize int       `json:"page_size,omitempty"`
}

// SweepWalletHistoryQuery is the body of /v1/sweeps/wallet/history — same
// shape plus the wallet address filter.
type SweepWalletHistoryQuery struct {
	Address  string    `json:"address"`
	Mode     SweepMode `json:"mode,omitempty"`
	Page     int       `json:"page,omitempty"`
	PageSize int       `json:"page_size,omitempty"`
}

// Sweep is one transit→master movement.
type Sweep struct {
	TaskID        string      `json:"task_id"`
	SweepTxHash   string      `json:"sweep_tx_hash,omitempty"`
	GasPumpTxHash string      `json:"gas_pump_tx_hash,omitempty"`
	Status        string      `json:"status"`
	WalletAddress string      `json:"wallet_address"`
	Chain         Chain       `json:"chain"`
	ChainFamily   ChainFamily `json:"chain_family,omitempty"`
	AssetSymbol   string      `json:"asset_symbol,omitempty"`
	AssetType     string      `json:"asset_type,omitempty"`
	AmountHuman   string      `json:"amount_human,omitempty"`

	// TypeWork is what triggered this sweep: momentum, threshold or force.
	TypeWork string `json:"type_work,omitempty"`

	// SweepConfirmations is how many confirmations the sweep transaction had
	// when the platform last looked, and CompletedAt when it reached the
	// network's confirmation target.
	//
	// Status tells the two apart. "broadcasted" means the transaction is out
	// and not yet confirmed; "completed" means confirmed. Until the platform
	// started reporting broadcast and confirmation separately, "completed"
	// meant only "sent, and no failure observed within three minutes" - so a
	// sweep could read completed while its transaction was still unconfirmed
	// or had been dropped.
	SweepConfirmations uint32 `json:"sweep_confirmations,omitempty"`
	CompletedAt        string `json:"completed_at,omitempty"`

	// Fees. TotalFeeUSD is the whole cost of the sweep; the gas-pump half is
	// the funding transfer that pays for it on chains that need one. The
	// Real* figures are what the chain actually charged, filled in once the
	// transaction settles; the others are the estimate made up front.
	TotalFeeUSD         string `json:"total_fee_usd,omitempty"`
	GasPumpSource       string `json:"gas_pump_source,omitempty"`
	GasPumpFeeHuman     string `json:"gas_pump_fee_human,omitempty"`
	GasPumpFeeUSD       string `json:"gas_pump_fee_usd,omitempty"`
	SweepFeeHuman       string `json:"sweep_fee_human,omitempty"`
	SweepFeeUSD         string `json:"sweep_fee_usd,omitempty"`
	RealGasPumpFeeHuman string `json:"real_gas_pump_fee_human,omitempty"`
	RealGasPumpFeeUSD   string `json:"real_gas_pump_fee_usd,omitempty"`
	RealSweepFeeHuman   string `json:"real_sweep_fee_human,omitempty"`
	RealSweepFeeUSD     string `json:"real_sweep_fee_usd,omitempty"`

	CreatedAt string `json:"created_at,omitempty"`

	// Deprecated: never populated. The platform reports fees under the names
	// above; these three were guesses at a shape it does not send. Kept so
	// existing code still compiles.
	GasFeeHuman    string `json:"gas_fee_human,omitempty"`
	GasFeeFiat     string `json:"gas_fee_fiat,omitempty"`
	ServiceFeeFiat string `json:"service_fee_fiat,omitempty"`
	// Deprecated: never populated - sweep history carries created_at and, once
	// confirmed, completed_at.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Sweep status values. A sweep is broadcast first and confirmed after; see the
// note on Sweep.SweepConfirmations for why the distinction matters.
const (
	SweepStatusPending     = "pending"
	SweepStatusWaitingGas  = "waiting_gas"
	SweepStatusBroadcasted = "broadcasted"
	SweepStatusCompleted   = "completed"
	SweepStatusFailed      = "failed"
	// SweepStatusSkipped is a sweep the platform decided against - most often a
	// balance below the threshold. It is a normal outcome, not a failure.
	SweepStatusSkipped = "skipped"
)

// SweepHistoryResponse is the page of sweeps.
type SweepHistoryResponse struct {
	Items []Sweep     `json:"items"`
	Meta  HistoryMeta `json:"meta"`
}

// ForceSweepResponse is the synchronous ack of /v1/sweeps/force. The actual
// transit→master movement happens asynchronously; poll WalletHistory to
// observe the resulting Sweep record.
type ForceSweepResponse struct {
	Status string `json:"status"`
}

// Force triggers an immediate transit→master sweep for one address. The
// status acknowledges acceptance; the resulting Sweep record appears via
// WalletHistory once the on-chain tx is built and submitted.
func (s *SweepsService) Force(ctx context.Context, address string, network Chain) (*ForceSweepResponse, error) {
	body := map[string]string{
		"address":      address,
		"network_code": string(network),
	}
	var out ForceSweepResponse
	if err := s.c.do(ctx, "/v1/sweeps/force", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// History returns recent sweeps across the whole project.
func (s *SweepsService) History(ctx context.Context, q SweepHistoryQuery) (*SweepHistoryResponse, error) {
	var out SweepHistoryResponse
	if err := s.c.do(ctx, "/v1/sweeps/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WalletHistory returns recent sweeps scoped to one wallet.
func (s *SweepsService) WalletHistory(ctx context.Context, q SweepWalletHistoryQuery) (*SweepHistoryResponse, error) {
	var out SweepHistoryResponse
	if err := s.c.do(ctx, "/v1/sweeps/wallet/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ============ Auto-sweep settings ============

// The sweep modes, as the platform accepts them in SweepSettingsUpdate.TypeWork.
//
//   - SweepModeOff: the wallet is never swept on its own. Force still works.
//   - SweepModeMomentum: sweep as soon as funds arrive.
//   - SweepModeThreshold: sweep once the balance reaches ThresholdUSD. The
//     platform re-checks a held balance periodically, so a wallet that crosses
//     the threshold through price movement alone is still swept.
const (
	SweepModeOff       = "turned_off"
	SweepModeMomentum  = "momentum"
	SweepModeThreshold = "threshold"
)

// Who pays the gas for a sweep.
//
//   - SweepFeeModeClient: taken from the swept wallet itself.
//   - SweepFeeModeService: paid by the platform's service wallet.
//   - SweepFeeModeMix: the service wallet funds the gas and the cost is
//     reclaimed from the sweep.
const (
	SweepFeeModeClient  = "client"
	SweepFeeModeService = "service"
	SweepFeeModeMix     = "mix"
)

// SweepPolicy is a resolved set of sweep rules.
type SweepPolicy struct {
	TypeWork string `json:"type_work"`
	// ThresholdUSD is meaningful only when TypeWork is SweepModeThreshold.
	ThresholdUSD string `json:"threshold_amount_usd,omitempty"`
	FeeMode      string `json:"fee_mode"`
	// Source says where the mode came from: wallet_network, wallet, project or
	// default. Present on SweepSettings.Effective, which is the only place the
	// question arises.
	Source string `json:"source,omitempty"`
}

// SweepOverride is what one wallet decides for itself. Every field is a pointer
// because nil means "not overridden - inherit it", which no value can express.
type SweepOverride struct {
	// NetworkCode empty means the override covers the address on every network
	// it exists on; set, it covers that one network and takes precedence over
	// the address-wide override.
	NetworkCode  string  `json:"network_code"`
	TypeWork     *string `json:"type_work"`
	ThresholdUSD *string `json:"threshold_amount_usd"`
	FeeMode      *string `json:"fee_mode"`
	// Source is who wrote it: merchant or operator.
	Source string `json:"source"`
	// Locked means an operator pinned this policy. While it is set, a merchant
	// write answers SWEEP_SETTINGS_LOCKED and changes nothing.
	Locked bool `json:"locked"`
}

// SweepSettings is the answer of /v1/sweeps/settings: three layers, on purpose.
//
// Effective is what will actually happen. Override is what this wallet decides
// for itself, or nil if it decides nothing. ProjectDefault is what it falls back
// to. Only the three together answer "is this value mine or inherited" - which
// is the difference between changing it here and changing it on the project.
//
// Inheritance is per field, not per row: a wallet can override the mode and keep
// inheriting the fee mode.
type SweepSettings struct {
	WalletAddress  string         `json:"wallet_address,omitempty"`
	NetworkCode    string         `json:"network_code,omitempty"`
	Effective      SweepPolicy    `json:"effective"`
	Override       *SweepOverride `json:"override"`
	ProjectDefault SweepPolicy    `json:"project_default"`
}

// SweepSettingsQuery is the body of /v1/sweeps/settings. An empty Address asks
// for the project's own default rather than any wallet's policy.
type SweepSettingsQuery struct {
	Address     string `json:"address,omitempty"`
	NetworkCode Chain  `json:"network_code,omitempty"`
}

// SweepSettingsUpdate is the body of /v1/sweeps/settings/update.
//
// A nil field is left alone. To stop overriding a field and go back to
// inheriting it, name it in Fields and leave its value nil - that is the only
// way to drop one field while keeping the others, which is why Fields exists at
// all.
type SweepSettingsUpdate struct {
	Address     string `json:"address"`
	NetworkCode Chain  `json:"network_code,omitempty"`

	// Fields names what this call is writing: type_work, threshold_amount_usd,
	// fee_mode. Optional when every field being written carries a value.
	Fields []string `json:"fields,omitempty"`

	TypeWork     *string `json:"type_work,omitempty"`
	ThresholdUSD *string `json:"threshold_amount_usd,omitempty"`
	FeeMode      *string `json:"fee_mode,omitempty"`
}

// Settings returns the auto-sweep policy in force for one wallet, together with
// what it overrides and what it inherits.
//
// The policy is scoped to the caller's own wallets: an address that is not the
// project's answers WALLET_NOT_FOUND.
func (s *SweepsService) Settings(ctx context.Context, q SweepSettingsQuery) (*SweepSettings, error) {
	var out SweepSettings
	if err := s.c.do(ctx, "/v1/sweeps/settings", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSettings writes a wallet's auto-sweep policy and returns the settings as
// they stand afterwards, so the caller can see what the write resolved to
// without asking again.
//
// Refusals are named: TYPE_WORK_INVALID, FEE_MODE_INVALID, THRESHOLD_INVALID,
// THRESHOLD_MUST_BE_POSITIVE, THRESHOLD_REQUIRED_FOR_THRESHOLD_MODE, and
// SWEEP_SETTINGS_LOCKED when an operator has pinned the policy.
func (s *SweepsService) UpdateSettings(ctx context.Context, in SweepSettingsUpdate) (*SweepSettings, error) {
	var out SweepSettings
	if err := s.c.do(ctx, "/v1/sweeps/settings/update", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
