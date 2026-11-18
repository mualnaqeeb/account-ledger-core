# AMBIGUITIES.md — Specification Ambiguities & Resolutions

During design and implementation, the following ten ambiguities were identified in the specification and resolved explicitly in code.

---

### 1. Out-of-Order Stream Position of `E10` (`Day 5` Listed After `E9` `Day 6`)
- **Ambiguity**: The stream states *"Event stream, replayed in this order"* with `E9` (`Day 6`) preceding `E10` (`Day 5 — CREDIT — ACC-002 BHD 10.000 — value_date Day 5`). If Day 5 closes immediately when `E9` (`Day 6`) triggers the day boundary, `E10` has not yet been ingested when EOD 5 executes.
- **Resolution**: `ReplayStream` honors strict stream order (`E1..E9..E10`). `E10` is tagged `BookingDay=5`, `ValueDate=5`, and `IngestedDay=6`. Day 5's emitted `DayReport` remains immutable (showing `ACC-002 = BHD 0.000` at stream position after `E8`), and when `CloseDay(6)` runs prior to interest capitalization, `reconcilePriorDayAccrual` appends a supplementary Day 5 `DailyInterestAccrual` (`BHD +0.004`) derived from `LedgerBalanceForBusinessDay(ACC-002, 5)`. Thus `ACC-002` accrues both Day 5 (`0.004`) and Day 6 (`0.004`), capitalizing `BHD 0.008` without mutating any record.

### 2. Daily Interest Accrual Basis Under Backdated Postings (`E7`, `E9`)
- **Ambiguity**: Does `"Daily interest: 0.04% per day on the closing ledger balance"` mean:
  - **(a) Point-in-time EOD accrual** (locked at each day's EOD cutoff: `0.10 + 0.10 + 0.26 + 0.19 + 0.00 + 0.16 = AED 0.81`), or
  - **(b) Retroactive value-date recomputation at Day 6** (recomputing Days 2–5 from reconciled value-date balances `[250.00, 225.00, 625.00, 415.00, 390.00, 390.00]` = `0.10 + 0.09 + 0.25 + 0.17 + 0.16 + 0.16 = AED 0.93`)?
- **Resolution**: Adopted **Point-in-Time EOD Accrual** via `LedgerBalanceForBusinessDay(accID, day)` (`BookingDay <= day && ValueDate <= day`). Each business day locks its interest base at EOD cutoff (`AED 0.81` total for `ACC-001`), so backdated `E7` (`BookingDay=5`) does not retroactively strip interest already locked on Days 2–4, and `E9` (`BookingDay=6`) does not retroactively manufacture Day 5 interest after `ACC-001` closed Day 5 in overdraft (`AED -230.00`).

### 3. Cascading Value-Date Overdraft Fees Across Multiple Days (`E7`)
- **Ambiguity**: When `E7` (`value_date Day 2`, `-620.00`) causes Day 2 to go negative (`-370.00`), an `AED 25.00` fee is booked with `value_date = Day 2`. Does that Day 2 fee participate in the closing balance evaluation (`value_date <= d`) of Days 3, 4, and 5?
- **Resolution**: **Yes.** Per the rule *"when that day's closing ledger balance (all entries with value_date <= that day) is negative. Booked with value_date equal to the day assessed"*, `assessOverdraftFeesUpTo` iterates `vd = 1..maxDay` strictly in ascending order and commits each fee (`value_date = vd`) immediately into `entries`. Consequently:
  - Day 2 (`-370.00`) triggers `-25.00` (`value_date=2`) -> Day 2 closes at `-395.00`.
  - Day 3 (`-395.00 + 400.00 = +5.00`) stays positive and avoids a fee.
  - Day 4 (`+5.00 - 185.00 = -180.00`) triggers `-25.00` (`value_date=4`) -> Day 4 closes at `-205.00`.
  - Day 5 (`-205.00`) triggers `-25.00` (`value_date=5`) -> Day 5 closes at `-230.00`.

### 4. Whether `E9` (Reversal of `E7`) Also Reverses Assessed Overdraft Fees
- **Ambiguity**: When `E9` reverses `E7` on Day 6, should the engine append compensating fee refunds for the three `E7`-induced overdraft fees (`AED 75.00`), or reverse only `E7`?
- **Resolution**: `E9` reverses **only `E7`'s principal** (`+AED 620.00`, `value_date=2`). Overdraft fees were validly triggered by Day 5's closing state under an append-only ledger and remain intact (`ACC-001` Day 6 pre-interest balance = `AED 390.00`, post-interest = `AED 390.81`).

### 5. Balance Definition Used for Intraday Authorization (`E8`) After Backdated Debit (`E7`)
- **Ambiguity**: Does an intraday authorization (`E8`, Day 5) evaluate `ledger balance minus active holds` across all posted entries regardless of `value_date`, or only `value_date == currentDay`?
- **Resolution**: `AvailableBalance` uses **all posted entries** (`LedgerBalance - ActiveHolds`). Because `E7` was already posted prior to `E8` on Day 5, `ACC-001`'s ledger balance is `465.00 - 620.00 = AED -155.00`, so `Auth-B` (`90.00`) is correctly `DECLINED` (`-245.00 < 0`). Note that `design_limit_test.go` exposes a subtle edge case of this choice when an unassessed fee from a backdated debit has not yet posted at EOD.

### 6. Hold Release When Settlement Amount Differs From Hold Amount (`E5`)
- **Ambiguity**: `Auth-A` held `AED 200.00` (`E3`), while `E5` settles `Auth-A` for `AED 185.00` (leaving `AED 15.00` difference). Should `AED 15.00` stay held for partial settlements or be released completely?
- **Resolution**: Standard single-capture authorization settlement: `E5` settles `Auth-A` completely (`Status -> AuthSettled`), releasing the entire active hold (`HoldDelta = -200.00`) and posting a `SETTLEMENT` debit of `AED -185.00`. Subsequent settlement attempts referencing `Auth-A` are rejected (`ErrAuthAlreadySettled`).

### 7. Rounding Mode & Capitalization Reconciliation for Daily Interest
- **Ambiguity**: `0.04%` on `AED 465.00` (`0.186`) and `AED 390.00` (`0.156`) produces fractional fils, and the prompt warns against remainder mismatches between daily accruals and capitalization.
- **Resolution**: Daily interest is rounded at each currency's native scale (`2` dp for `AED`, `3` dp for `BHD`) using `ROUND_HALF_EVEN` (`ComputeDailyInterest` in `ledger/money.go`), and the single Day 6 capitalization credit is computed directly as the exact sum of the rounded daily accruals (`sumAccrualsForAccount`), guaranteeing zero discrepancy and zero discarded remainder.

### 8. Intra-Day Ordering on Day 6: Daily Accrual Before Capitalization
- **Ambiguity**: On Day 6, does Day 6's own `0.04%` daily accrual include the Day 6 capitalization credit itself, or is Day 6 interest accrued on the pre-capitalization balance (`390.00`) and then capitalized?
- **Resolution**: `CloseDay(6)` computes Day 6's daily accrual on the pre-capitalization balance (`390.00 * 0.04% = AED 0.16`, `10.000 * 0.04% = BHD 0.004`), then posts `INT-CAP-D6-ACC-001` (`+0.81`) and `INT-CAP-D6-ACC-002` (`+0.008`) as the final closing entries. Interest never compounds onto itself within the same closing tick.

### 9. Currency Applicability of the `AED 25.00` Overdraft Fee Rule
- **Ambiguity**: The non-negotiable rule specifies `"Overdraft fee: AED 25.00"` without specifying an `AED/BHD` FX rate if `ACC-002` (`BHD`) were ever overdrawn.
- **Resolution**: In `assessOverdraftFeesUpTo`, `OverdraftFeeMinorAED` applies only to `CurrencyAED` accounts (`ACC-001`). Non-AED accounts are not assessed synthetic fee amounts without an authoritative FX rate table. (`ACC-002` stays `>= 0.000` throughout Days 1–6.)

### 10. Remainder Allocation Order in Equal-Instalment Splitting (`E10`)
- **Ambiguity**: Splitting `BHD 10.000` (`10,000 fils`) into 3 equal instalments leaves `1 fil` (`0.001 BHD`) indivisible (`10000 = 3 * 3333 + 1`). Which instalment receives the extra fil (`[3.334, 3.333, 3.333]` vs `[3.333, 3.333, 3.334]`)?
- **Resolution**: Adopted standard leading remainder allocation (`SplitInstalments` assigns `+1` minor unit to the first `total % count` entries: `BHD 3.334, BHD 3.333, BHD 3.333`). Both orderings sum to `BHD 10.000`, keep all parts within 1 minor unit, and post on the same `ValueDate = 5`.
