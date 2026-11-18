# NUMBERS.md — Every Chosen Constant & Why Not Half

Every numeric constant in `ledger/constants.go`, `ledger/stream.go`, `ledger/ledger_test.go`, and `ledger/design_limit_test.go` is catalogued below, along with its exact rationale and what breaks if the constant were halved.

---

## 1. Domain & Configuration Constants (`ledger/constants.go`)

| Constant | Value | Why This Exact Value | Why Not Half It |
|---|---|---|---|
| `ScaleAED` | `2` | ISO-4217 AED subdivides into 100 fils (`10^-2`). Preserves exact cent/fil accuracy for `185.00`, `25.00`, `0.19`, `0.81`. | Half (`1`) truncates AED to tenths (`0.10`), turning `AED 0.19` into `0.2` and making exact fil-level accounting impossible. |
| `ScaleBHD` | `3` | ISO-4217 BHD subdivides into 1,000 fils (`10^-3`), required for `BHD 10.000`, `BHD 3.334`, and `BHD 0.004` interest. | Integer half (`1`) loses 100x precision; even `1.5` (impossible integer scale) or `2` collapses `0.004` interest to `0.00` and violates the 3-decimal rule. |
| `MinorUnitsAED` | `100` | `10^ScaleAED`: exact minor units per 1 AED so `1,200.00 AED = 120,000` fils. | Half (`50`) breaks decimal place alignment (`50` minor units = `AED 1.00` breaks formatting, base-10 rounding, and `4 / 10000` percentage scaling). |
| `MinorUnitsBHD` | `1000` | `10^ScaleBHD`: exact minor units per 1 BHD (`10.000 BHD = 10,000` fils). | Half (`500`) makes `3333` minor units represent `6.666 BHD` instead of `3.333 BHD`, corrupting `FormatAmount` and `SplitInstalments`. |
| `WindowStartDay` | `1` | Business days in the specification start at `Day 1` (`Day 1 through Day 6`). | Integer half (`0`) introduces a phantom `Day 0` with spurious idle accruals and overdraft checks before `E1` exists. |
| `WindowEndDay` | `6` | Specification defines a 6-day replay window (`Day 1` through `Day 6`) with interest capitalization at end of Day 6. | Half (`3`) stops halfway through the stream, skipping `E5` settlement, `E6` rejection, `E7` overdraft cascade, `E8` decline, `E9` reversal, `E10` instalments, and Day 6 capitalization. |
| `OverdraftFeeMinorAED` | `2500` | Non-negotiable rule mandates flat `AED 25.00` (`2,500` fils) fee per negative value-date day. | Half (`1250` = `AED 12.50`) undercharges every overdraft day by 50%, changing Day 5 closing balance from `AED -230.00` to `AED -192.50` and post-`E9` balance from `AED 390.00` to `AED 427.50`. |
| `MaxOverdraftFeesPerAccountPerDay` | `1` | Non-negotiable rule specifies overdraft fee is *"assessed once per day per account"*. | Integer half (`0`) disables overdraft assessment entirely (`0` fees posted on Day 5, leaving EOD 5 balance at `AED -155.00` instead of `AED -230.00`). |
| `DailyInterestRateNumerator` | `4` | Exact numerator of `0.04% = 0.0004 = 4 / 10,000` using integer arithmetic without floats. | Half (`2`) yields a `0.02%` daily rate, halving interest across the board (`AED 0.81 -> 0.41`, `BHD 0.008 -> 0.004`). |
| `DailyInterestRateDenominator` | `10000` | Exact denominator of `0.04% = 4 / 10,000` (`100 * 100` basis points). | Half (`5000`) doubles the daily interest rate to `0.08%`, doubling all interest accruals (`AED 0.81 -> 1.61`, `BHD 0.008 -> 0.016`). |
| `MinAvailableBalanceAfterHoldMinor` | `0` | Non-negotiable rule: *"available balance — ledger balance minus active holds — remains at or above zero"*. | Half of `0` is `0`; any negative relaxation violates the rule (allowing unauthorized credit extension), while any positive floor rejects exact-zero holds like `100.00 - 100.00 = 0.00`. |
| `DefaultInstalmentCount` | `1` | Standard single-posting events (`E1..E9`) create 1 ledger entry. | Integer half (`0`) causes a division-by-zero / validation error in `SplitInstalments`, rejecting every standard credit. |
| `E10InstalmentCount` | `3` | `E10` specifies *"posted as three equal instalments"*. | Integer half (`1`) posts a lump sum `BHD 10.000` (`2` splits `5.000 + 5.000`), completely missing the `3.334 / 3.333 / 3.333` remainder split test. |

---

## 2. Canonical Event Stream Constants (`ledger/stream.go`)

| Event / Field | Minor Units | Human Value | Why This Value / Why Not Half |
|---|---|---|---|
| `E1` Amount | `120000` | `AED 1,200.00` | Given in specification. Half (`600.00`) fails `E2` (`950.00`), immediately putting `ACC-001` into `-350.00` overdraft on Day 1 and failing `Auth-A`. |
| `E2` Amount | `95000` | `AED 950.00` | Given in specification. Half (`475.00`) leaves Day 1 at `725.00` instead of `250.00`, altering Day 2–4 interest and stopping `E7` (`620.00`) from making Day 2 negative (`725 - 620 = +105.00`). |
| `E3` (`Auth-A`) | `20000` | `AED 200.00` | Given in specification. Half (`100.00`) yields `150.00` available on Day 2 instead of `50.00`, breaking the exact tight headroom check (`250 - 200 = 50`). |
| `E4` Amount | `40000` | `AED 400.00` | Given in specification. Half (`200.00`) fails to bring Day 3 above zero after `E7` + Day 2 fee (`-395.00 + 200.00 = -195.00`), causing a 4th overdraft fee on Day 3! |
| `E5` (`Auth-A` settle) | `18500` | `AED 185.00` | Given in specification. Half (`92.50`) leaves Day 4 at `557.50` pre-`E7` and `-102.50` post-`E7`, diverging from the canonical balances. |
| `E6` (`Auth-Z` settle) | `18000` | `AED 180.00` | Given in specification. Half (`90.00`) is equally rejected (`Auth-Z` absent), but diverges from the event stream definition. |
| `E7` Amount | `62000` | `AED 620.00` | Given in specification. Half (`310.00`) fails to drive Day 2 negative (`250.00 - 310.00 = -60.00` still triggers Day 2, but Day 4 `+340.00 - 185.00 = +155.00` stays positive!), erasing the Day 4 & Day 5 overdraft fees and failing to prove Criterion 2 wrong (`1` fee vs `3`). |
| `E8` (`Auth-B`) | `9000` | `AED 90.00` | Given in specification. Half (`45.00`) is still declined on `-155.00`, but diverges from the canonical event amount. |
| `E10` Amount | `10000` | `BHD 10.000` | Given in specification. Half (`5.000 BHD = 5,000 fils`) divides by `3` as `1.667 + 1.667 + 1.666` (two rounded-up instalments instead of one) and halves Day 5/6 interest to `BHD 0.002`. |

---

## 3. Unit Test & Design-Limit Test Constants (`ledger/*_test.go`)

| Test File | Value | Purpose | Why Not Half |
|---|---|---|---|
| `ledger_test.go` (`Auth-Exact` / `Auth-Over`) | `10000` (`AED 100.00`) & `1` (`AED 0.01`) | Exercises the exact `0.00` approval boundary and `1` minor-unit (`-0.01`) decline boundary. | Half of `1` minor unit (`0` in integer math) makes `Auth-Over` a zero-amount invalid request instead of a boundary overdraft decline. |
| `ledger_test.go` (`TestPrecisionFormatting...`) | `12500`, `1250`, `3750`, `46500` | Tests exact `0.04%` products `5.0`, `0.5` (even -> `0`), `1.5` (odd -> `2`), and `18.6 -> 19`. | Half of `1250` (`625 * 4 / 10000 = 0.25`) is no longer a `.5` tie, failing to exercise `ROUND_HALF_EVEN` tie-breaking! |
| `design_limit_test.go` | `10000` (`D1-C1`), `12000` (`D5-E1`), `3000` (`D5-E2`), `1000` (`D5-E3`) | Minimum clean round AED amounts where `100 - 120 = -20 < 0` triggers a Day 4 fee (`-25`), `30` gives a positive *raw* intraday balance (`+10`) while true post-fee solvency is negative (`-15`), and `10` requests the entire phantom `+10`. | Halving `D5-E1` (`60.00`) leaves Day 4 positive (`100 - 60 = +40`), preventing the retroactive fee; halving `D5-E2` (`15.00`) makes raw posted balance negative (`-5.00`), so intraday auth declines normally without exposing the race; halving `D5-E3` (`5.00`) still reproduces the bug (`+10 - 5 = +5`), though `10.00` demonstrates full exhaustion of the phantom buffer. |
