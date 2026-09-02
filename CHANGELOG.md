# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
