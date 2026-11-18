# REJECTED.md — Refused Acceptance Criteria & Abandoned Approaches

## Part 1: Acceptance Criteria Disposition

The problem statement warned that some listed acceptance criteria are incorrect. Below is the disposition of all eight criteria, with exact mathematical proofs and rule references for every refusal.

---

### Summary Table

| # | Criterion | Disposition | Primary Reason |
|---|---|---|---|
| 1 | Day 2 closing ledger balance at EOD 5 before fees is `AED -370.00` | **ACCEPTED** | `1200.00 - 950.00 - 620.00 = -370.00` (`E1 + E2 + E7`). |
| 2 | `E7` causes exactly one overdraft fee to be assessed, on Day 2 | **REJECTED** | `E7` drives Days 2, 4, and 5 negative (`3` fees = `AED 75.00`). |
| 3 | Day 4 settlement of `Auth-A` must be accepted | **ACCEPTED** | `Auth-A` (`AED 200.00` hold) exists and settles for `AED 185.00`. |
| 4 | Any settlement referencing an unknown auth ID is rejected; no funds leave | **ACCEPTED** | `Auth-Z` (`E6`) fails with `UNKNOWN_AUTHORIZATION`; balance stays `AED 465.00`. |
| 5 | If `Auth-B` is approved, its hold reduces available balance but not ledger balance | **REJECTED IN PREMISE** (Invariant Accepted) | The hold invariant holds (`Auth-A` on Day 2), but `Auth-B` (`E8`) **must be declined** on Day 5 (`available -155.00 - 90.00 = -245.00 < 0`). |
| 6 | After `E9`, all balances and fees return to their pre-`E7` values | **REJECTED** | Append-only ledger retains `AED 75.00` fees; pre-interest Day 6 balance is `AED 390.00 != AED 465.00`; `Auth-B` remains `DECLINED`. |
| 7 | The three BHD instalments in `E10` must each be `BHD 3.334` | **REJECTED** | `3 * 3.334 = BHD 10.002 != BHD 10.000` (creates `0.002 BHD`). |
| 8 | If rounded daily interest accruals do not sum to capitalized total, discard remainder | **REJECTED** | Directly violates Non-Negotiable Rule: rounded daily accruals must sum **exactly** to the capitalized total. |

---

### Detailed Refusals

#### Criterion 2 — REFUSED
> *"E7 causes exactly one overdraft fee to be assessed, on Day 2."*

**Why it is wrong:**  
The overdraft rule states: *"Overdraft fee: AED 25.00, assessed once per day per account when that day's closing ledger balance (all entries with value_date <= that day) is negative. Booked with value_date equal to the day assessed."*

When `E7` (`DEBIT AED 620.00`, `booking_day=5`, `value_date=2`) posts on Day 5, the value-date closing balances for `ACC-001` evaluated at EOD 5 in ascending `value_date` order are:
- **Day 1 (`value_date <= 1`)**: `1,200.00 - 950.00 = AED +250.00` (>= 0, no fee).
- **Day 2 (`value_date <= 2`)**: `250.00 - 620.00 = AED -370.00` (< 0 -> **Fee #1** `AED -25.00`, `value_date=2`). Post-fee Day 2 balance: `AED -395.00`.
- **Day 3 (`value_date <= 3`)**: `-395.00 + 400.00 (E4) = AED +5.00` (>= 0, spared!).
- **Day 4 (`value_date <= 4`)**: `+5.00 - 185.00 (E5) = AED -180.00` (< 0 -> **Fee #2** `AED -25.00`, `value_date=4`). Post-fee Day 4 balance: `AED -205.00`.
- **Day 5 (`value_date <= 5`)**: `-205.00 = AED -205.00` (< 0 -> **Fee #3** `AED -25.00`, `value_date=5`). Post-fee Day 5 balance: `AED -230.00`.

Even if a naive implementation checked `E7` in isolation without cascading Day 2's `-25.00` fee into Day 4, Day 4's pre-fee balance (`250 - 620 + 400 - 185`) would still be `AED -155.00 < 0` and Day 5 would still be `AED -155.00 < 0`. Under both views, `E7` causes **three** overdraft fees (`Days 2, 4, 5`, totaling `AED 75.00`), never one.

---

#### Criterion 5 — ACCEPTED AS GENERAL INVARIANT / REFUSED AS `Auth-B` PREMISE
> *"If Auth-B is approved, its hold reduces available balance but not ledger balance."*

**Why the premise is refused for `E8`:**  
As a general accounting law, an approved authorization hold reduces available balance (`ledger - active_holds`) while leaving ledger balance unchanged — which our engine verifies with `Auth-A` (`E3` on Day 2: ledger `AED 250.00`, hold `AED 200.00`, available `AED 50.00`) and in `TestAuthorizationBoundaryAndHoldInvariants`.

However, within the canonical event stream, **`Auth-B` (`E8`, Day 5, `AED 90.00`) cannot be approved**:
- `E7` (`DEBIT AED 620.00`) occurs on Day 5 immediately before `E8` (`Auth-B`).
- At the moment `E8` arrives, `ACC-001`'s posted ledger balance is `465.00 - 620.00 = AED -155.00`, with `AED 0.00` active holds.
- Applying an `AED 90.00` hold would leave `available = -155.00 - 90.00 = AED -245.00 < 0.00`, violating the non-negotiable rule: *"An authorization is approved only if the account's available balance — ledger balance minus active holds — remains at or above zero after the hold is applied."*
- Therefore `Auth-B` must be **`DECLINED`** (`TestCanonicalReplay_AcceptedCriteriaAndAllDays` asserts `AuthDeclined`).

---

#### Criterion 6 — REFUSED
> *"After E9, all balances and fees return to their pre-E7 values."*

**Why it is wrong:**  
1. **Append-only immutability**: The ledger cannot delete or mutate the three `OVERDRAFT_FEE` entries (`AED 75.00` total) posted at EOD 5.
2. **Scope of `E9`**: `E9` (`Day 6 — REVERSAL — ACC-001 reverses E7 — value_date Day 2`) only posts a compensating `+AED 620.00` credit reversing `E7`. It does not waive or refund assessed overdraft fees.
3. **Numerical discrepancy**: Pre-`E7` closing ledger balance (at EOD 4) was `AED 465.00` with `AED 0.00` fees. After `E9`, the pre-interest Day 6 balance is `465.00 - 75.00 = AED 390.00` (and `AED 390.81` after Day 6 interest capitalization), with `AED 75.00` in permanent fee history across value dates Day 2 (`AED 225.00`), Day 4 (`AED 415.00`), and Day 5 (`AED 390.00`).
4. **Irreversible authorization side-effect**: Because `E7` pushed `ACC-001` negative during Day 5, `Auth-B` (`E8`) was permanently `DECLINED` on Day 5 (whereas without `E7`, `465.00 - 90.00 = 375.00 >= 0` would have approved `Auth-B`).

---

#### Criterion 7 — REFUSED
> *"The three BHD instalments in E10 must each be BHD 3.334."*

**Why it is wrong:**  
`BHD` uses 3 decimal places (`1 BHD = 1,000 fils`). Three entries of `BHD 3.334` equal:
$$\text{3.334} + \text{3.334} + \text{3.334} = \text{BHD 10.002} \neq \text{BHD 10.000}$$
This creates `BHD 0.002` (`2 fils`) out of thin air and fails double-entry/conservation accounting. Using deterministic remainder allocation (`SplitInstalments`), `10,000 / 3 = 3,333 remainder 1`, yielding **`BHD 3.334`**, **`BHD 3.333`**, **`BHD 3.333`**, which sum identically to `BHD 10.000`.

---

#### Criterion 8 — REFUSED
> *"If the rounded daily interest accruals do not sum to the capitalized total, the remainder is discarded."*

**Why it is wrong:**  
This directly contradicts the explicit Non-Negotiable Rule:
> *"Accruals capitalize as a single credit at end of Day 6. The rounded daily accruals must sum exactly to the capitalized total."*

Discarding a remainder breaks that equality invariant. In our design (`ledger/engine.go`), each day's `0.04%` accrual is rounded to currency precision via `ComputeDailyInterest` (half-to-even), and the single Day 6 capitalization credit is defined as the **exact sum** of those rounded daily accruals:
- `ACC-001`: `0.10 + 0.10 + 0.26 + 0.19 + 0.00 + 0.16 = AED 0.81` (capitalized entry = `AED 0.81`, discrepancy = `AED 0.00`).
- `ACC-002`: `0.000 + 0.000 + 0.000 + 0.000 + 0.004 + 0.004 = BHD 0.008` (capitalized entry = `BHD 0.008`, discrepancy = `BHD 0.000`).

---

## Part 2: Approaches Abandoned Mid-Build

1. **IEEE-754 `float64` Monetary Representation**  
   - *Why considered*: Fast built-in arithmetic in Go.  
   - *Why abandoned*: `float64` cannot represent decimal fractions like `0.01`, `0.001`, or `10.000 / 3` exactly, producing silent ULP drift during compounding and rounding comparisons (`0.186` vs `0.18599999999999998`). Replaced entirely with fixed-point `int64` minor units (`Amount`) in `ledger/money.go`.

2. **Mutable In-Memory `Account.Balance` and `Authorization` Structs**  
   - *Why considered*: Updating `account.Balance += delta` and toggling `auth.Status = "SETTLED"` in a map is simpler to write initially.  
   - *Why abandoned*: Direct state mutation violates *"The ledger is append-only. No event record is ever mutated or deleted"* and cannot reconstruct historical point-in-time `value_date <= d` balances when backdated entries (`E7`, `E9`) arrive later. Replaced with immutable append-only `entries`, `authEvents`, `fees`, `accruals`, and `errors` slices plus pure projection functions (`LedgerBalanceByValueDate`, `AuthorizationStates`).

3. **Pre-Sorting Input Events by `BookingDay` Before Replay**  
   - *Why considered*: Sorting `E1..E10` by `BookingDay` would place `E10` (Day 5) ahead of `E9` (Day 6) and simplify the Day 5 EOD loop.  
   - *Why abandoned*: Violates the explicit specification *"Event stream, replayed in this order"* where `E10` is event #10 after `E9`. Instead, `ReplayStream` processes events in exact stream order and `CloseDay(6)` performs append-only late-booking reconciliation via `reconcilePriorDayAccrual` without mutating Day 5's already-emitted `DayReport`.

4. **Single-Day Event Model (No Separation Between `BookingDay`, `IngestedDay`, and `ValueDate`)**  
   - *Why considered*: Simpler `InputEvent` struct with only one `Day` integer.  
   - *Why abandoned*: Fails to distinguish *when an event was booked* (`E7` on Day 5, `E9` on Day 6), *when it arrived in stream order* (`E10` `BookingDay=5`, `IngestedDay=6`), and *its economic value date* (`ValueDate=2` for `E7` and `E9`). Separating all three fields on `LedgerEntry` makes both point-in-time EOD queries and value-date balance audits unambiguous.
