package ledger

import (
	"strings"
	"testing"
)

func TestCanonicalReplay_AcceptedCriteriaAndAllDays(t *testing.T) {
	eng, reports, err := RunCanonicalReplay()
	if err != nil {
		t.Fatalf("RunCanonicalReplay failed: %v", err)
	}
	if len(reports) != WindowEndDay {
		t.Fatalf("expected %d daily reports, got %d", WindowEndDay, len(reports))
	}

	// Day 1: E1 (+1200.00) + E2 (-950.00) = 250.00
	d1 := summaryByAccount(t, reports[0], AccountID1)
	assertAmount(t, "Day 1 ACC-001 ledger", d1.ClosingLedgerBal, 25000)
	assertAmount(t, "Day 1 ACC-001 holds", d1.ActiveHolds, 0)
	assertAmount(t, "Day 1 ACC-001 available", d1.AvailableBalance, 25000)
	assertAmount(t, "Day 1 ACC-001 interest", d1.InterestAccrued, 10)

	// Day 2: Auth-A hold 200.00 reduces available to 50.00 without altering ledger balance (250.00)
	d2 := summaryByAccount(t, reports[1], AccountID1)
	assertAmount(t, "Day 2 ACC-001 ledger", d2.ClosingLedgerBal, 25000)
	assertAmount(t, "Day 2 ACC-001 holds", d2.ActiveHolds, 20000)
	assertAmount(t, "Day 2 ACC-001 available", d2.AvailableBalance, 5000)
	assertAmount(t, "Day 2 ACC-001 interest", d2.InterestAccrued, 10)

	// Day 3: E4 (+400.00) -> ledger 650.00, available 450.00
	d3 := summaryByAccount(t, reports[2], AccountID1)
	assertAmount(t, "Day 3 ACC-001 ledger", d3.ClosingLedgerBal, 65000)
	assertAmount(t, "Day 3 ACC-001 holds", d3.ActiveHolds, 20000)
	assertAmount(t, "Day 3 ACC-001 available", d3.AvailableBalance, 45000)
	assertAmount(t, "Day 3 ACC-001 interest", d3.InterestAccrued, 26)

	// Day 4: E5 settles Auth-A for 185.00 (Criterion 3) and releases 200.00 hold; E6 Auth-Z rejected (Criterion 4)
	d4 := summaryByAccount(t, reports[3], AccountID1)
	assertAmount(t, "Day 4 ACC-001 ledger", d4.ClosingLedgerBal, 46500)
	assertAmount(t, "Day 4 ACC-001 holds", d4.ActiveHolds, 0)
	assertAmount(t, "Day 4 ACC-001 available", d4.AvailableBalance, 46500)
	assertAmount(t, "Day 4 ACC-001 interest", d4.InterestAccrued, 19)

	if len(reports[3].ErrorsToday) != 1 || reports[3].ErrorsToday[0].Code != ErrUnknownAuthorization {
		t.Fatalf("expected exactly one ErrUnknownAuthorization on Day 4, got %+v", reports[3].ErrorsToday)
	}
	if reports[3].ErrorsToday[0].EventID != "E6" {
		t.Fatalf("expected Day 4 error for E6, got %q", reports[3].ErrorsToday[0].EventID)
	}

	// Day 5: E7 (-620.00 vd=2), Auth-B declined, 3 overdraft fees assessed (D2, D4, D5 = -75.00)
	d5 := summaryByAccount(t, reports[4], AccountID1)
	assertAmount(t, "Day 5 ACC-001 ledger", d5.ClosingLedgerBal, -23000)
	assertAmount(t, "Day 5 ACC-001 holds", d5.ActiveHolds, 0)
	assertAmount(t, "Day 5 ACC-001 interest", d5.InterestAccrued, 0)
	if len(d5.FeesAssessedToday) != 3 {
		t.Fatalf("expected 3 overdraft fees on Day 5 (D2, D4, D5), got %d", len(d5.FeesAssessedToday))
	}
	if len(reports[4].ErrorsToday) != 1 || reports[4].ErrorsToday[0].Code != ErrInsufficientFunds {
		t.Fatalf("expected Auth-B decline error on Day 5, got %+v", reports[4].ErrorsToday)
	}

	// Day 6: E9 reverses E7 (+620.00 vd=2), fees (-75.00) remain, capitalized interest +0.81 -> 390.81
	d6Acc1 := summaryByAccount(t, reports[5], AccountID1)
	assertAmount(t, "Day 6 ACC-001 interest", d6Acc1.InterestAccrued, 16)
	assertAmount(t, "Day 6 ACC-001 capitalized", d6Acc1.CapitalizedInterest, 81)
	assertAmount(t, "Day 6 ACC-001 ledger", d6Acc1.ClosingLedgerBal, 39081)

	// Day 6 ACC-002: E10 (10.000 BHD) + capitalized interest (0.004 D5 + 0.004 D6 = 0.008) -> 10.008
	d6Acc2 := summaryByAccount(t, reports[5], AccountID2)
	assertAmount(t, "Day 6 ACC-002 interest", d6Acc2.InterestAccrued, 4)
	assertAmount(t, "Day 6 ACC-002 capitalized", d6Acc2.CapitalizedInterest, 8)
	assertAmount(t, "Day 6 ACC-002 ledger", d6Acc2.ClosingLedgerBal, 10008)

	// Verify final authorization states
	authMap := make(map[string]AuthorizationState)
	for _, st := range eng.AuthorizationStates() {
		authMap[st.AuthID] = st
	}
	if authMap["Auth-A"].Status != AuthSettled || authMap["Auth-A"].SettledAmount != 18500 || authMap["Auth-A"].ActiveHold != 0 {
		t.Fatalf("unexpected Auth-A final state: %+v", authMap["Auth-A"])
	}
	if authMap["Auth-B"].Status != AuthDeclined || authMap["Auth-B"].ActiveHold != 0 {
		t.Fatalf("unexpected Auth-B final state: %+v", authMap["Auth-B"])
	}
	if _, exists := authMap["Auth-Z"]; exists {
		t.Fatalf("Auth-Z must not exist in authorization states")
	}
}

func TestCriterion1_Day2BalanceAtEndOfDay5BeforeFees(t *testing.T) {
	eng := NewDefaultEngine()
	stream := CanonicalEventStream()

	// Replay E1..E8 without running CloseDay(5) yet (before any Day 5 overdraft fee assessment).
	for _, ev := range stream {
		if ev.BookingDay <= 4 {
			if err := eng.ApplyEvent(ev); err != nil {
				t.Fatalf("ApplyEvent %s failed: %v", ev.ID, err)
			}
		}
	}
	for d := 1; d <= 4; d++ {
		if _, err := eng.CloseDay(d, nil); err != nil {
			t.Fatalf("CloseDay(%d) failed: %v", d, err)
		}
	}
	// Apply Day 5 events E7 and E8
	if err := eng.ApplyEvent(stream[6]); err != nil { // E7
		t.Fatalf("ApplyEvent E7 failed: %v", err)
	}
	if err := eng.ApplyEvent(stream[7]); err != nil { // E8
		t.Fatalf("ApplyEvent E8 failed: %v", err)
	}

	day2PreFee, err := eng.LedgerBalanceByValueDate(AccountID1, 2)
	if err != nil {
		t.Fatalf("LedgerBalanceByValueDate failed: %v", err)
	}
	// 1200.00 - 950.00 - 620.00 = -370.00 (-37000 minor units)
	assertAmount(t, "Day 2 closing balance at EOD 5 before fees", day2PreFee, -37000)
	if len(eng.Fees()) != 0 {
		t.Fatalf("expected 0 fees before CloseDay(5), got %d", len(eng.Fees()))
	}

	// Now run CloseDay(5) and verify the 3 fees (D2, D4, D5) and Day 3 non-overdraft
	rep5, err := eng.CloseDay(5, []string{"E7", "E8"})
	if err != nil {
		t.Fatalf("CloseDay(5) failed: %v", err)
	}
	d5Acc1 := summaryByAccount(t, rep5, AccountID1)
	if len(d5Acc1.FeesAssessedToday) != 3 {
		t.Fatalf("expected 3 fees, got %d", len(d5Acc1.FeesAssessedToday))
	}
	expectedFeeDays := []int{2, 4, 5}
	for i, expDay := range expectedFeeDays {
		if d5Acc1.FeesAssessedToday[i].ValueDate != expDay {
			t.Fatalf("fee[%d] expected value_date Day %d, got Day %d", i, expDay, d5Acc1.FeesAssessedToday[i].ValueDate)
		}
	}
}

func TestCriterion7_E10InstalmentsSumExactlyTo10BHD(t *testing.T) {
	eng, _, err := RunCanonicalReplay()
	if err != nil {
		t.Fatalf("RunCanonicalReplay failed: %v", err)
	}

	var e10Entries []LedgerEntry
	var sum Amount
	for _, entry := range eng.Entries() {
		if entry.SourceEventID == "E10" {
			e10Entries = append(e10Entries, entry)
			sum += entry.Delta
		}
	}
	if len(e10Entries) != 3 {
		t.Fatalf("expected 3 entries for E10, got %d", len(e10Entries))
	}
	expected := []Amount{3334, 3333, 3333}
	for i, exp := range expected {
		assertAmount(t, "E10 instalment", e10Entries[i].Delta, exp)
	}
	assertAmount(t, "E10 instalment sum", sum, 10000)
}

func TestCriterion8_InterestAccrualsSumExactlyToCapitalizedTotal(t *testing.T) {
	eng, _, err := RunCanonicalReplay()
	if err != nil {
		t.Fatalf("RunCanonicalReplay failed: %v", err)
	}

	for _, acc := range eng.Accounts() {
		var accrualSum Amount
		for _, a := range eng.Accruals() {
			if a.AccountID == acc.ID {
				accrualSum += a.AccruedAmount
			}
		}
		var capEntryTotal Amount
		capCount := 0
		for _, entry := range eng.Entries() {
			if entry.AccountID == acc.ID && entry.Kind == EntryInterestCap {
				capCount++
				capEntryTotal += entry.Delta
			}
		}
		if capCount != 1 {
			t.Fatalf("expected 1 capitalization credit for %s, got %d", acc.ID, capCount)
		}
		if accrualSum != capEntryTotal {
			t.Fatalf("%s rounded accruals sum %d != capitalized total %d", acc.ID, accrualSum, capEntryTotal)
		}
	}
}

func TestRejectedCriterion6_ReversalDoesNotDeleteAssessedFeesOrRestorePreE7Balance(t *testing.T) {
	eng, _, err := RunCanonicalReplay()
	if err != nil {
		t.Fatalf("RunCanonicalReplay failed: %v", err)
	}

	// Pre-E7 closing ledger balance on Day 4 was AED 465.00 (46500).
	// Post-E9 pre-interest balance on Day 6 is AED 390.00 (39000) because 3 fees (AED 75.00) remain.
	preInterestDay6 := Amount(0)
	for _, entry := range eng.Entries() {
		if entry.AccountID == AccountID1 && entry.Kind != EntryInterestCap {
			preInterestDay6 += entry.Delta
		}
	}
	assertAmount(t, "ACC-001 post-E9 pre-interest balance", preInterestDay6, 39000)
	if len(eng.Fees()) != 3 {
		t.Fatalf("expected 3 append-only overdraft fee records to remain after E9, got %d", len(eng.Fees()))
	}
}

func TestAuthorizationBoundaryAndHoldInvariants(t *testing.T) {
	eng := NewDefaultEngine()
	_ = eng.ApplyEvent(InputEvent{
		ID: "C1", BookingDay: 1, ValueDate: 1, Type: EventCredit, AccountID: AccountID1, Amount: 10000,
	})
	// Exact zero remaining available balance (100.00 - 100.00 = 0.00) must be APPROVED
	_ = eng.ApplyEvent(InputEvent{
		ID: "A1", BookingDay: 1, ValueDate: 1, Type: EventAuthorization, AccountID: AccountID1, AuthID: "Auth-Exact", Amount: 10000,
	})
	// Ledger balance unchanged at 100.00, available balance is 0.00
	bal, _ := eng.LedgerBalance(AccountID1)
	avail, _ := eng.AvailableBalance(AccountID1)
	assertAmount(t, "ledger after hold", bal, 10000)
	assertAmount(t, "available after exact hold", avail, 0)

	// One minor unit over (0.01) must be DECLINED
	_ = eng.ApplyEvent(InputEvent{
		ID: "A2", BookingDay: 1, ValueDate: 1, Type: EventAuthorization, AccountID: AccountID1, AuthID: "Auth-Over", Amount: 1,
	})
	states := eng.AuthorizationStates()
	if len(states) != 2 {
		t.Fatalf("expected 2 auth states, got %d", len(states))
	}
	if states[0].Status != AuthApproved || states[1].Status != AuthDeclined {
		t.Fatalf("unexpected auth states: %+v", states)
	}
}

func TestAppendOnlyImmutabilityAndDefensiveCopies(t *testing.T) {
	eng, reports, err := RunCanonicalReplay()
	if err != nil {
		t.Fatalf("RunCanonicalReplay failed: %v", err)
	}

	entries := eng.Entries()
	origDelta := entries[0].Delta
	entries[0].Delta = 99999999

	reports[0].Accounts[0].ClosingLedgerBal = -123456

	freshEntries := eng.Entries()
	if freshEntries[0].Delta != origDelta {
		t.Fatalf("mutating returned Entries() slice altered internal engine state")
	}
	freshReports := eng.DayReports()
	if freshReports[0].Accounts[0].ClosingLedgerBal == -123456 {
		t.Fatalf("mutating returned DayReports() slice altered internal engine state")
	}
	for i := 1; i < len(freshEntries); i++ {
		if freshEntries[i].Sequence <= freshEntries[i-1].Sequence {
			t.Fatalf("entry sequence not strictly increasing at index %d", i)
		}
	}
}

func TestPrecisionFormattingAndDailyInterestRounding(t *testing.T) {
	if got := FormatAmount(120000, CurrencyAED); got != "AED 1,200.00" {
		t.Fatalf("FormatAmount AED mismatch: got %q", got)
	}
	if got := FormatAmount(-37000, CurrencyAED); got != "AED -370.00" {
		t.Fatalf("FormatAmount negative AED mismatch: got %q", got)
	}
	if got := FormatAmount(10008, CurrencyBHD); got != "BHD 10.008" {
		t.Fatalf("FormatAmount BHD mismatch: got %q", got)
	}

	// Half-to-even (banker's rounding) verification:
	// 125.00 AED (12500) * 0.0004 = 5.0000 -> 5 (0.05)
	assertAmount(t, "interest 125.00 AED", ComputeDailyInterest(12500), 5)
	// 12.50 AED (1250) * 0.0004 = 0.5000 -> 0 (even)
	assertAmount(t, "interest 12.50 AED half-to-even", ComputeDailyInterest(1250), 0)
	// 37.50 AED (3750) * 0.0004 = 1.5000 -> 2 (even)
	assertAmount(t, "interest 37.50 AED half-to-even", ComputeDailyInterest(3750), 2)
	// 465.00 AED (46500) * 0.0004 = 18.6 -> 19 (0.19)
	assertAmount(t, "interest 465.00 AED", ComputeDailyInterest(46500), 19)
	// Negative / zero balance -> 0
	assertAmount(t, "interest negative", ComputeDailyInterest(-37000), 0)
	assertAmount(t, "interest zero", ComputeDailyInterest(0), 0)
}

func TestErrorPathsAndFormatReplayReport(t *testing.T) {
	eng := NewDefaultEngine()
	_ = eng.ApplyEvent(InputEvent{ID: "C1", BookingDay: 1, ValueDate: 1, Type: EventCredit, AccountID: AccountID1, Amount: 50000})
	_ = eng.ApplyEvent(InputEvent{ID: "A1", BookingDay: 1, ValueDate: 1, Type: EventAuthorization, AccountID: AccountID1, AuthID: "Auth-1", Amount: 10000})
	_ = eng.ApplyEvent(InputEvent{ID: "S1", BookingDay: 1, ValueDate: 1, Type: EventSettlement, AccountID: AccountID1, AuthID: "Auth-1", Amount: 10000})
	_ = eng.ApplyEvent(InputEvent{ID: "S2", BookingDay: 1, ValueDate: 1, Type: EventSettlement, AccountID: AccountID1, AuthID: "Auth-1", Amount: 5000})
	_ = eng.ApplyEvent(InputEvent{ID: "R1", BookingDay: 1, ValueDate: 1, Type: EventReversal, AccountID: AccountID1, TargetEventID: "C1"})
	_ = eng.ApplyEvent(InputEvent{ID: "R2", BookingDay: 1, ValueDate: 1, Type: EventReversal, AccountID: AccountID1, TargetEventID: "C1"})

	errs := eng.Errors()
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors (double settlement + double reversal), got %+v", errs)
	}
	if errs[0].Code != ErrAuthAlreadySettled || errs[1].Code != ErrAlreadyReversed {
		t.Fatalf("unexpected error codes: %+v", errs)
	}

	canonEng, canonReports, err := RunCanonicalReplay()
	if err != nil {
		t.Fatalf("RunCanonicalReplay failed: %v", err)
	}
	output := FormatReplayReport(canonEng, canonReports)
	for _, expectedSubstr := range []string{
		"DAY 1", "DAY 6", "AED 390.81", "BHD 10.008",
		"Auth-A", "Auth-B", "UNKNOWN_AUTHORIZATION", "INSUFFICIENT_AVAILABLE_BALANCE",
		"FEE-OD-ACC-001-D2", "FEE-OD-ACC-001-D4", "FEE-OD-ACC-001-D5",
		"BHD 3.334", "BHD 3.333",
	} {
		if !strings.Contains(output, expectedSubstr) {
			t.Fatalf("FormatReplayReport output missing %q", expectedSubstr)
		}
	}
}

func summaryByAccount(t *testing.T, rep DayReport, accountID string) AccountDaySummary {
	t.Helper()
	for _, a := range rep.Accounts {
		if a.AccountID == accountID {
			return a
		}
	}
	t.Fatalf("account %s not found in Day %d report", accountID, rep.Day)
	return AccountDaySummary{}
}

func assertAmount(t *testing.T, label string, got, want Amount) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %d, want %d", label, got, want)
	}
}
