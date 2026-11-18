package ledger

import "testing"

// TestDesignLimit_IntradayAuthBlindToPendingRetroactiveOverdraftFee is the single
// intentional failing test against our own design.
//
// WHAT THIS TEST REVEALS ABOUT OUR DESIGN:
//
//  1. Architectural Coupling Between Batch EOD Fee Assessment and Real-Time Authorization:
//     Our Engine strictly separates intraday posting (ApplyEvent) from End-of-Day batch
//     assessment (CloseDay). Overdraft fees are only evaluated and posted when CloseDay(d)
//     executes, because the spec defines the fee trigger as "when that day's closing ledger
//     balance (all entries with value_date <= that day) is negative" — meaning an intraday
//     deficit on Day 5 might still be cured by a same-day credit before EOD 5.
//
//  2. The Blind Spot Created by Backdated Entries (ValueDate < CurrentBookingDay):
//     Once business Day 5 begins, Day 4's EOD window has already elapsed. If a backdated
//     debit arrives on Day 5 with ValueDate=4 (driving Day 4's closing balance to AED -20.00),
//     Day 4's overdraft fee (AED 25.00) is already mathematically inescapable unless a
//     same-day-4-valued credit/reversal arrives — yet under our batch CloseDay(5) design,
//     that AED 25.00 liability does not exist in the posted ledger until CloseDay(5) runs.
//
//  3. Economic Consequence:
//     In the scenario below, Day 5 receives:
//       - D5-E1: DEBIT AED 120.00 with ValueDate=4 (Day 4 balance drops from +100.00 to -20.00)
//       - D5-E2: CREDIT AED 30.00 with ValueDate=5 (posted ledger balance becomes +10.00)
//       - D5-E3: AUTHORIZATION Auth-Race for AED 10.00 with ValueDate=5
//     Because our Engine computes AvailableBalance solely from posted LedgerEntries minus
//     ActiveHolds, it sees +10.00 available and APPROVES Auth-Race (+10.00 - 10.00 = 0.00).
//     Immediately afterward at CloseDay(5), Day 4's unassessed fee (-25.00) and Day 5's
//     consequent fee (-25.00) post, plunging the account to AED -40.00 BEFORE Auth-Race even
//     settles (-50.00 once settled).
//
//  4. How a Production Core Would Fix This:
//     Maintain a continuous "shadow pending-fee reserve" for finalized past value dates
//     (vd < currentDay) whose closing balance has dropped below zero, deducting accrued
//     unposted fee liabilities from AvailableBalance during intraday authorization checks
//     without prematurely booking immutable fee entries before EOD cutoff.
func TestDesignLimit_IntradayAuthBlindToPendingRetroactiveOverdraftFee(t *testing.T) {
	eng := NewDefaultEngine()

	// Day 1: Open with AED 100.00 credit and close Days 1..4 cleanly.
	if err := eng.ApplyEvent(InputEvent{
		ID: "D1-C1", BookingDay: 1, ValueDate: 1, Type: EventCredit, AccountID: AccountID1, Amount: 10000,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for d := 1; d <= 4; d++ {
		if _, err := eng.CloseDay(d, nil); err != nil {
			t.Fatalf("CloseDay(%d) failed: %v", d, err)
		}
	}

	// Day 5 intraday step 1: Backdated debit drives Day 4's closing balance to AED -20.00.
	// This guarantees an AED 25.00 overdraft fee for value_date Day 4 at EOD 5.
	if err := eng.ApplyEvent(InputEvent{
		ID: "D5-E1", BookingDay: 5, ValueDate: 4, Type: EventDebit, AccountID: AccountID1, Amount: 12000,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Day 5 intraday step 2: Same-day credit brings raw posted balance to AED +10.00.
	// Note that even with +30.00, true post-Day-4-fee solvency is -20.00 - 25.00 + 30.00 = -15.00.
	if err := eng.ApplyEvent(InputEvent{
		ID: "D5-E2", BookingDay: 5, ValueDate: 5, Type: EventCredit, AccountID: AccountID1, Amount: 3000,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Day 5 intraday step 3: Intraday authorization requests AED 10.00 hold.
	// Our design ignores the unbooked Day 4 overdraft fee liability and approves Auth-Race.
	if err := eng.ApplyEvent(InputEvent{
		ID: "D5-E3", BookingDay: 5, ValueDate: 5, Type: EventAuthorization, AccountID: AccountID1, AuthID: "Auth-Race", Amount: 1000,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close Day 5 so the inevitable Day 4 and Day 5 overdraft fees materialize (-50.00 total fees),
	// exposing that Auth-Race was approved on phantom available funds (-40.00 post-EOD balance).
	rep5, err := eng.CloseDay(5, []string{"D5-E1", "D5-E2", "D5-E3"})
	if err != nil {
		t.Fatalf("CloseDay(5) failed: %v", err)
	}

	var authRace AuthorizationState
	for _, st := range rep5.AuthorizationStates {
		if st.AuthID == "Auth-Race" {
			authRace = st
		}
	}

	// This assertion intentionally fails against our current engine design:
	// Expected economically sound outcome: AuthDeclined (true net solvency was AED -15.00).
	// Actual engine outcome: AuthApproved (blind to unbooked retroactive EOD fee AED -25.00).
	if authRace.Status != AuthDeclined {
		t.Fatalf("DESIGN LIMIT EXPOSED: Auth-Race status = %s (posted available before EOD fees masked "+
			"AED -25.00 retroactive Day 4 fee obligation; EOD 5 closing balance plunged to %s)",
			authRace.Status, FormatAmount(rep5.Accounts[0].ClosingLedgerBal, CurrencyAED))
	}
}
