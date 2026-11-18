# WORKLOG.md — Engineering Session Log

All timestamps are local time (`+02:00`, `2026-11-18`).

---

## 20:46:59 – 20:54:01 | Environment Verification & Requirements Analysis
- Audited workspace tools (`gh` authenticated as `mualnaqeeb`, `git`, `go 1.26.1`).
- Parsed all 6-day ledger rules, `E1..E10` stream ordering, and the 8 acceptance criteria.
- Identified 4 defective acceptance criteria (`#2` single overdraft fee, `#5` premature `Auth-B` approval assumption, `#6` reversal restoring pre-`E7` balances/fees, `#7` `3 * 3.334 BHD`, `#8` discarding interest remainder) and 10 domain ambiguities (including `E10` out-of-order stream arrival and bitemporal `BookingDay` vs `ValueDate` interaction).

## 20:54:28 – 20:59:38 | Design Alignment with User
- Presented key architectural choices to the user and locked decisions:
  - **Language**: Go (`go 1.26`, standard library only, `int64` fixed-point minor units).
  - **Overdraft & Reversal (`E7`/`E9`)**: Assess 3 overdraft fees at EOD 5 (`Days 2, 4, 5` = `AED 75.00`); `E9` reverses only `E7` principal (`+620.00`), preserving assessed fees on the append-only ledger (`AED 390.00` pre-interest Day 6 balance).
  - **Interest Basis**: Point-in-time EOD daily accrual (`0.10 + 0.10 + 0.26 + 0.19 + 0.00 + 0.16 = AED 0.81` -> `AED 390.81`; `ACC-002` `0.004 + 0.004 = BHD 0.008` -> `BHD 10.008`).
  - **GitHub Target**: Public repo `mualnaqeeb/account-ledger-core`.

## 21:00:46 – 21:01:17 | Commit `4e7d9e1` — Core Constants, Fixed-Point Money, and Immutable Models
- Initialized `/Users/mualnaqeeb/wd/account-ledger-core` on branch `main`.
- Implemented `go.mod`, `ledger/constants.go`, `ledger/money.go` (`SplitInstalments`, `ComputeDailyInterest` half-to-even, `FormatAmount`), and `ledger/models.go` (`LedgerEntry`, `AuthEventRecord`, `FeeAssessment`, `DailyInterestAccrual`, `LedgerError`, `DayReport`).
- Ran `go vet ./ledger` (exit code 0) and committed `4e7d9e1`.

## 21:01:20 – 21:02:29 | Commit `7a291d3` — Append-Only Bitemporal Ledger Engine
- Implemented `ledger/engine.go` (`Engine`) with defensive slice copy getters, bitemporal balance projections (`LedgerBalance`, `LedgerBalanceByValueDate`, `LedgerBalanceForBusinessDay`), authorization hold folding, `E1..E10` validation, ascending value-date overdraft assessment (`assessOverdraftFeesUpTo`), and Day 6 late-booking catch-up accrual + exact-sum interest capitalization.
- Ran `go vet ./ledger` (exit code 0) and committed `7a291d3`.

## 21:02:35 – 21:03:28 | Commit `d319470` — Canonical Event Stream & CLI Reporter
- Created `ledger/stream.go` and `cmd/replay/main.go`.
- Ran `go run ./cmd/replay` and verified per-day balances (`ACC-001`: `250.00 -> 250.00 -> 650.00 -> 465.00 -> -230.00 -> 390.81`; `ACC-002`: `0.000 -> 10.008`), 3 overdraft fees on Day 5, `Auth-A` settlement, `Auth-Z` rejection, `Auth-B` decline, `E10` split (`3.334, 3.333, 3.333`), and exact interest sum parity.
- Refined `stream.go` fee sign formatting and Day 6 late-booking catch-up annotation, re-verified `go run ./cmd/replay`, and committed `d319470`.

## 21:03:35 – 21:04:03 | Commit `95c0800` — Passing Unit & Replay Verification Suite
- Created `ledger/ledger_test.go` covering 9 test suites across replay correctness, pre-fee Day 2 `-370.00` verification, `E10` instalment conservation, `sum(rounded accruals) == capitalized total`, `E9` non-waiver of fees, authorization `0.00` boundary, defensive slice immutability, banker's rounding ties, and error handling.
- Executed `go test -v ./ledger` — 9/9 tests passed (`0.458s`). Committed `95c0800`.

## 21:04:10 – 21:04:38 | Commit `72aa43b` — Annotated Failing Design-Limit Test
- Authored `ledger/design_limit_test.go` (`TestDesignLimit_IntradayAuthBlindToPendingRetroactiveOverdraftFee`), demonstrating how batch EOD fee deferral causes intraday authorization checks to ignore inevitable retroactive overdraft fee liabilities from finalized earlier value dates (`Auth-Race` approved on `AED +10.00` raw balance even though Day 4 `AED -25.00` fee guarantees insolvency).
- Verified `go test -v ./ledger -skip TestDesignLimit` passes (9/9) and `go test -v ./ledger -run TestDesignLimit` fails with `DESIGN LIMIT EXPOSED`. Committed `72aa43b`.

## 21:04:45 – 21:05:48 | Commit `b890b55` — Rejection Proofs, Ambiguities, and Constants Audit
- Authored `REJECTED.md` (5 criterion rejections/premise distinctions + 4 abandoned engineering approaches), `AMBIGUITIES.md` (10 resolved specification ambiguities), and `NUMBERS.md` (every domain, stream, and test constant + why not half).
- Committed `b890b55`.

## 21:05:54 – Present | Final Documentation, Verification & GitHub Publication
- Wrote `README.md` and finalized `WORKLOG.md` using verified commit hashes and timestamps.
- Next: commit documentation, re-run full verification (`go run ./cmd/replay`, passing test suite, failing design-limit test), and push intact multi-commit `main` branch to public GitHub repository `mualnaqeeb/account-ledger-core`.
