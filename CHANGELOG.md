# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
