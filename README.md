# In-Memory Account Ledger Core (`account-ledger-core`)

An in-memory, append-only, bitemporal account ledger core written in pure Go (`go 1.26`, standard library only — zero external dependencies, zero web layer, zero persistence, zero UI, zero database).

---

## Architecture Overview

- **Fixed-Point Monetary Arithmetic (`ledger/money.go`)**: All monetary quantities use `int64` minor units (`Amount`) to eliminate IEEE-754 floating-point drift. `AED` uses scale `2` (`100` fils per AED) and `BHD` uses scale `3` (`1,000` fils per BHD).
- **Strictly Append-Only Core (`ledger/engine.go`)**: State mutations are forbidden. Credits, debits, settlements, reversals, overdraft fees, and Day 6 interest capitalization are appended as immutable `LedgerEntry` records; authorization lifecycle transitions (`APPROVED`, `SETTLED`, `DECLINED`) append immutable `AuthEventRecord` entries; declined or invalid events append `LedgerError` records.
- **Bitemporal Accounting**: Every entry tracks `BookingDay`, `IngestedDay`, and `ValueDate` independently, enabling exact backdated value-date balance evaluations (`E7`, `E9`), cascading value-date overdraft assessment (`CloseDay`), and out-of-order stream replay (`E10` arriving after `E9`).
- **Exact Interest Capitalization**: `0.04%` daily interest is calculated on positive closing balances using banker's rounding (`ROUND_HALF_EVEN`) at each currency's native scale, and capitalized as a single credit at End of Day 6 equal to the **exact sum** of the rounded daily accruals.

---

## Quick Start & Commands

Requires **Go 1.26** (or Go 1.21+ standard library).

### 1. Run the 6-Day Event Stream Replay

```bash
go run ./cmd/replay
```

This replays `E1` through `E10` in exact specification order across `Day 1` through `Day 6` for `ACC-001` (`AED`) and `ACC-002` (`BHD`), printing per-day closing ledger balances, fee assessments, authorization states, interest accruals/capitalization, and errors, followed by the end-of-window reconciled summary and append-only ledger entry log.

### 2. Run the Passing Verification Suite

```bash
go test -v ./ledger -skip TestDesignLimit
```

Runs the 9 unit and replay verification tests in `ledger/ledger_test.go`, validating all accepted criteria, non-negotiable invariants, append-only immutability, instalment splitting (`3.334 + 3.333 + 3.333 = 10.000`), rounding, and rejected criterion contradictions.

### 3. Run the Annotated Failing Design-Limit Test

```bash
go test -v ./ledger -run TestDesignLimit
```

Runs `TestDesignLimit_IntradayAuthBlindToPendingRetroactiveOverdraftFee` in `ledger/design_limit_test.go`. This test intentionally fails against our design to expose the boundary between batch EOD retroactive fee assessment and real-time intraday authorization checks.

*(Running plain `go test ./ledger` runs all 10 tests, producing 9 passes and the 1 intentional design-limit failure.)*

---

## How to Read the Replay Output

Each day block (`--- DAY N (Events ingested in cycle: ...) ---`) reports four sections:

1. **Closing Balances (`value_date <= day`)**:
   - `Ledger`: Closing ledger balance for `value_date <= day` at that day's EOD cutoff.
   - `Holds`: Sum of currently active authorization holds (`APPROVED` and not yet `SETTLED`).
   - `Available`: `Ledger - Holds`.
   - `Daily Interest`: `0.04%` daily accrual computed on that day's positive closing balance (rounded to `AED 0.01` / `BHD 0.001`). On Day 6, also prints any late-booking prior-day catch-up (`BHD 0.004` for `E10`) and the single `Capitalized` credit posted at EOD 6.
2. **Fee Assessments**:
   - Lists any `AED 25.00` overdraft fees posted at EOD `N`, showing the triggered `value_date`, pre-fee balance, fee delta (`AED -25.00`), and resulting post-fee balance.
3. **Authorization States**:
   - Current projected status (`APPROVED`, `SETTLED`, `DECLINED`), original `hold`, current `active_hold`, and `settled` amount for every authorization ID (`Auth-A`, `Auth-B`). Unknown IDs (`Auth-Z`) never enter the authorization state projection.
4. **Errors**:
   - Rejected or declined operations recorded that day with structured error codes (`UNKNOWN_AUTHORIZATION` for `E6` on Day 4; `INSUFFICIENT_AVAILABLE_BALANCE` for `E8` on Day 5).

Following Day 6:
- **`END-OF-WINDOW RECONCILED SUMMARY`** shows the final value-dated balance curve (`Day 1..Day 6`) incorporating backdated postings (`E7`, `E9`, `E10`) and proves `Sum of rounded daily accruals == Capitalized total` (`AED 0.81` for `ACC-001`; `BHD 0.008` for `ACC-002`).
- **`APPEND-ONLY LEDGER ENTRY LOG`** prints all 14 immutable entries `#01` through `#14` with sequence numbers, `BookingDay`, `ValueDate`, and instalment indices (`inst=1/3`, `2/3`, `3/3`).

---

## Repository Documentation

- [`NUMBERS.md`](NUMBERS.md) — Every constant chosen, why that exact value, and why not half it.
- [`AMBIGUITIES.md`](AMBIGUITIES.md) — All 10 specification ambiguities identified and how each was resolved.
- [`REJECTED.md`](REJECTED.md) — Every refused acceptance criterion with mathematical proof, plus architectural approaches abandoned mid-build.
- [`WORKLOG.md`](WORKLOG.md) — Timestamped engineering log aligned with commit history.
