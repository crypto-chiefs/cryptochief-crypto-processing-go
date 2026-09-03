# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.0] - 2026-09-03

### Added

- `Client.Webhooks` — the platform's OUTBOUND webhooks, the deliveries it made to
  your endpoint. `Info` reads one delivery: status, every attempt with the HTTP
  status, duration, and **the body your endpoint answered** (capped, with a
  truncation flag), and the payload that was sent. `Resend` re-fires one
  delivery; `ResendStaticDeposit` re-fires the newest webhook of a static
  deposit by the deposit's own uuid. Same routes, bodies and refusal codes as
  the white-label platform (`/v1/webhooks/info`, `/v1/webhooks/resend`,
  `/v1/static-deposits/resend`), so one integration runs against either.
- `WebhookDeliveryHeader` (`X-Webhook-Delivery`) — the delivery uuid now rides
  on every webhook the platform sends. It is constant across attempts and
  resends of one delivery, so it doubles as your receiver's idempotency key,
  and it is the argument the new methods take. **Keep it when you log a
  webhook** — the API has no listing of deliveries, so it is the only way to
  name one afterwards.
- Refusal codes `CodeDeliverySuperseded`, `CodeDeliveryInFlight`,
  `CodeResendTooSoon`, `CodeNoDeliveries`, and `CodeNotFound`. A superseded
  delivery — one with a newer event for the same object — cannot be resent:
  re-sending `invoice.in_mempool` after `invoice.paid` would tell your system
  the order went backwards. The refusal names the newer event; resend that.

### Notes

- A resend on this platform is **synchronous**: the POST to your endpoint
  happens before the answer, so `Queued=true` arrives with `Status` already
  `delivered` or `failed` for that attempt, and `NextAttemptAt` is never set.
- A successful manual delivery is billed as `/v1/webhook/resend`; a refused one
  is not. `Info` is priced like the other reads.

## [0.7.0] - 2026-09-02

### Added

- `Wallets.PayInHistory` — `POST /v1/wallets/history`, every pay-in that used
  one deposit address. A deposit wallet can serve several orders over its
  lifetime, and this is the list of them, for when a payer gives you the address
  but not the order. The rows are the same `PayIn` records and the same
  `HistoryMeta` page as `PayIns.History`, not a parallel type. The address is
  matched case-insensitively, so either spelling of an EVM address works, and
  an address that is not your project's yields an empty page rather than an
  error.
- `Currencies.Fiats` — `POST /v1/currencies/fiats`, every fiat the platform can
  price an order in and quote a rate against: the codes to populate a currency
  selector with, and the values `Currencies.FiatToCrypto` and a FIAT-mode
  pay-in's `Currency` accept. Like `/v1/blockchains/list` it answers with a bare
  JSON array rather than the usual `{"items": …}` envelope, so the method
  returns a `[]FiatCurrency`.
- `Currencies.Cryptos` — `POST /v1/currencies/cryptos`, every crypto ticker the
  platform has a rate for, against USDT, with `ByExchange` saying which exchange
  carries which — the map `ConvertRequest.Provider` picks from.

  **Rate availability is not payment availability.** A ticker here is one the
  platform can put a price on; it says nothing about whether the project takes
  deposits, sweeps or payouts in it. That list is
  `Blockchain.ContractsAvailable`, and an asset picker built from this one
  offers customers assets orders will refuse.
- `Blockchain.SupportedChains` — `POST /v1/blockchains/list`, the chains the
  platform's scanner is currently connected to. It answers with a bare JSON
  array rather than the usual `{"items": …}` envelope, so the method returns a
  `[]SupportedChain`. `Name` is the CHAIN code; `Type` is the scanner's
  lowercase protocol family (`"evm"`, `"tron"`), which is not the `ChainFamily`
  spelling (`"EVM"`) responses elsewhere carry.
- `Blockchain.ContractsList` — `POST /v1/blockchain/contracts/list`, every coin
  and token the platform supports on every network, whatever this project has
  enabled: the catalogue to build a "which assets could we turn on" picker
  from. Same `AvailableContract` items as `Blockchain.ContractsAvailable`.
- `ChainFamily` and `IsTest` on `AvailableContract`. Both are sent by the
  catalogue and by the project's own list and were being dropped, so a picker
  had no way to tell a devnet asset from a real one. A native coin's
  `Contract` stays the empty string it arrives as.
- `Status` and `Search` on `SweepHistoryQuery` and `SweepWalletHistoryQuery`.
  `Status` narrows a page to one sweep status; left empty, every status is
  included, `skipped` ones among them. `Search` is a substring match — on the
  project-wide history it matches the wallet address, the sweep and gas-pump
  transaction hashes and the task ID; on the wallet variant, the hashes and the
  task ID.
- `GasSource` on the sweep settings, in all three layers and on the write:
  `SweepPolicy.GasSource` (concrete on `Effective`, so reading it tells you what
  will actually happen), `SweepOverride.GasSource` (a `*string`, `nil` meaning
  this layer does not decide — inherited, not switched off) and
  `SweepSettingsUpdate.GasSource`. `"gas_source"` is now accepted in the
  `Fields` mask, which is how the override is dropped so the wallet inherits
  again. `SweepGasSourceNative` and `SweepGasSourceRented` name the two values.

  It is TRON-only, and it answers a different question from `FeeMode`: what is
  bought for the transfer, not who pays its network fees. **Not setting it is
  not the same as setting `native`.** A wallet that has never chosen one gets
  the platform default, which is `rented` — the platform supplies the energy and
  bills it to your API credits, under any fee mode, without anybody having
  switched it on. Send `SweepGasSourceNative` explicitly to have the wallet burn
  its own TRX.

### Fixed

- `AvailableContract.IsTest` no longer carries `omitempty`. It is a `bool`, so
  the tag dropped `is_test: false` whenever a row was re-marshalled — collapsing
  "this is a mainnet asset" into "the field is absent", on the one field that
  separates a test asset from a real one. A mainnet row now re-serialises as
  `"is_test": false`. No other bool added in this release was affected: the
  `omitempty` bools on `EstimatePayoutRequest` are request fields, where
  omitting a false is the intended wire shape.
- `Blockchain.SupportedChains`, `Currencies.Fiats` and `Currencies.Cryptos`
  tolerate a `null` body. The three services build their result from a Go slice,
  so an **empty** result marshals as a literal `null` rather than `[]`. A method
  whose signature promises a list must answer that with an empty list, and these
  now do: `SupportedChains` and `Fiats` return an empty, non-nil slice, and
  `Cryptos` returns `Tickers`, `ByExchange` and every list inside `ByExchange`
  usable and empty — so `ByExchange` can be assigned to and not only read.
  Nothing errors, nothing decodes short, and re-marshalling a result writes `[]`
  and `{}` rather than passing the `null` on.
- The README's payout-webhook sample no longer reads `evt.TxID`, a field
  `PayoutWebhookEvent` has never had — the sample did not compile. A payout can
  draw on several source wallets, so the platform sends one `txid` per entry in
  `sources` and no top-level hash; the sample now says so and logs fields that
  exist.

### Changed

- The `SweepFeeMode*` doc comment described the wrong thing. The fee mode does
  not say who pays for a sweep outright: a deposit wallet holding enough of the
  chain's native coin pays for its own transfer whatever the mode, and the mode
  only decides who covers a **shortfall**. `client` takes it from your own master
  wallet — not from the swept wallet. `service` has the platform supply it **and
  bills the cost to your API credits**, which the comment omitted entirely.
  `mix` is **the default**, and it tries `client` first and falls back to
  `service` when the master wallet cannot cover it — it does not fund the gas
  from a service wallet and reclaim it from the sweep, which is what the comment
  claimed. Constants and wire values are unchanged.
- `Sweep.CompletedAt` is documented as what it is: stamped when the sweep reached
  a **terminal outcome, failures and skips included**. "Absent while still in
  flight" was true and dangerously incomplete — a failed sweep is not in flight
  either, so it carries `completed_at` too, and a reader who took presence for
  settlement would book a failed sweep as money received. That is why the sweep
  webhook carries a separate `ConfirmedAt` instead of reusing it. To tell
  settlement apart, check `SweepConfirmations` is above zero, or take
  `ConfirmedAt` from the `sweep.confirmed` webhook.

## [0.6.0] - 2026-09-02

### Added

- `Label` on `GenerateWalletRequest` and on every wallet response — a
  human-readable name of up to 255 characters ("hot wallet EU", "customer
  4242") for your own bookkeeping, on any wallet type rather than static ones
  only. It carries no routing meaning, is omitted from the wire when empty,
  and reads back as `w.Label` from generation, info, the list and the update
  calls alike.
- `Wallets.SetLabel` — `POST /v1/wallets/label`, renames a wallet after it has
  been created and returns the wallet as it stands afterwards. Master, transit
  and static wallets can all be renamed. An empty label is a value rather than
  an omission: it goes on the wire as `""` and takes the name away, which
  `Wallets.ClearLabel` spells out.
- `Wallets.SetCallbackURL` — `POST /v1/wallets/callback-url`, sets or clears
  the deposit webhook of a static wallet after creation, so a per-customer
  address no longer has to be re-created to move its callback. Static wallets
  only; an empty URL is likewise sent as `""` and clears the webhook, which
  `Wallets.ClearCallbackURL` spells out. The new URL applies to deposits
  announced from here on, not to ones already announced.
- `Wallets.RebindMaster` — `POST /v1/wallets/rebind-master`, re-points a
  transit or static wallet at another master of the same project and chain
  family. It moves no money: what changes is where the next sweep settles,
  including sweeps already queued but not yet sent. Idempotent — re-binding a
  wallet to the master it already has succeeds and changes nothing.
- `CodeLabelTooLong` (`LABEL_TOO_LONG`) — the code returned for a wallet label
  over 255 characters.

### Fixed

- Error codes the API gateway decides itself now reach the caller in
  `APIError.Code`. Those refusals put the machine code in the response's
  `error` field and an English sentence in `msg`, and the SDK read `msg`
  first — so `LABEL_TOO_LONG`, `INSUFFICIENT_CREDITS`, `DEBT_LIMIT_EXCEEDED`,
  `INVALID_PARAMS`, `UNKNOWN_FIELD`, `RATE_LIMITED` and the rest arrived as
  prose and never matched their constant. `Code` now takes `error` unless it
  is the generic `SERVICE_ERROR`, in which case the code is in `msg` as
  before. The `Code*` constants and the `errors.Is` sentinels work against
  both shapes; `Message` still carries the human sentence and `Raw` the whole
  body.

  If you worked around this by matching on the message text — comparing
  `Code` against `"label is longer than 255 characters"`, or digging the code
  out of `Raw` yourself — you will now get the machine code instead, and the
  comparison against the constant is the one to keep.

## [0.2.0] - 2026-08-18

### Added

- `Credits` service with `Balance` — `POST /v1/credits/balance`, the
  free-of-charge billing endpoint returning the project's credits / USD
  balance (USD can be negative on postpaid projects), the postpaid flag and
  debt limit, and the gas-operations gate state (`can_execute_gas_operations`,
  `gas_ops_min_credits`).
- `Credits.Topup` — `POST /v1/credits/topup`, creates a billing invoice
  (USDT/USDC, USD-pegged, max 100000) and returns a hosted payment link;
  optional `url_success` / `url_error` browser redirects are omitted from the
  wire when empty. Free of charge, rate-limited 60 req/min.
