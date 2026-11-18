package ledger

import (
	"fmt"
	"strings"
)

// CanonicalEventStream returns the exact 10-event sequence E1..E10 in replay order.
func CanonicalEventStream() []InputEvent {
	return []InputEvent{
		{
			ID:          "E1",
			BookingDay:  1,
			ValueDate:   1,
			Type:        EventCredit,
			AccountID:   AccountID1,
			Amount:      120000, // AED 1,200.00
			Description: "Initial credit",
		},
		{
			ID:          "E2",
			BookingDay:  1,
			ValueDate:   1,
			Type:        EventDebit,
			AccountID:   AccountID1,
			Amount:      95000, // AED 950.00
			Description: "Day 1 debit",
		},
		{
			ID:          "E3",
			BookingDay:  2,
			ValueDate:   2,
			Type:        EventAuthorization,
			AccountID:   AccountID1,
			Amount:      20000, // AED 200.00
			AuthID:      "Auth-A",
			Description: "Hold for Auth-A",
		},
		{
			ID:          "E4",
			BookingDay:  3,
			ValueDate:   3,
			Type:        EventCredit,
			AccountID:   AccountID1,
			Amount:      40000, // AED 400.00
			Description: "Day 3 credit",
		},
		{
			ID:          "E5",
			BookingDay:  4,
			ValueDate:   4,
			Type:        EventSettlement,
			AccountID:   AccountID1,
			Amount:      18500, // AED 185.00
			AuthID:      "Auth-A",
			Description: "Settlement of Auth-A",
		},
		{
			ID:          "E6",
			BookingDay:  4,
			ValueDate:   4,
			Type:        EventSettlement,
			AccountID:   AccountID1,
			Amount:      18000, // AED 180.00
			AuthID:      "Auth-Z",
			Description: "Orphan settlement referencing unknown Auth-Z",
		},
		{
			ID:          "E7",
			BookingDay:  5,
			ValueDate:   2,
			Type:        EventDebit,
			AccountID:   AccountID1,
			Amount:      62000, // AED 620.00
			Description: "Backdated debit (value_date Day 2)",
		},
		{
			ID:          "E8",
			BookingDay:  5,
			ValueDate:   5,
			Type:        EventAuthorization,
			AccountID:   AccountID1,
			Amount:      9000, // AED 90.00
			AuthID:      "Auth-B",
			Description: "Hold for Auth-B",
		},
		{
			ID:            "E9",
			BookingDay:    6,
			ValueDate:     2,
			Type:          EventReversal,
			AccountID:     AccountID1,
			TargetEventID: "E7",
			Description:   "Reversal of E7 (value_date Day 2)",
		},
		{
			ID:              "E10",
			BookingDay:      5,
			ValueDate:       5,
			Type:            EventCredit,
			AccountID:       AccountID2,
			Amount:          10000, // BHD 10.000
			InstalmentCount: E10InstalmentCount,
			Description:     "BHD 10.000 credit in three equal instalments (late Day 5 arrival)",
		},
	}
}

// ReplayStream replays events in strict slice order across the Day 1..Day 6 window.
func ReplayStream(eng *Engine, events []InputEvent) ([]DayReport, error) {
	currentDay := WindowStartDay
	var batchIDs []string

	for _, ev := range events {
		if ev.BookingDay > currentDay && ev.BookingDay <= WindowEndDay {
			if _, err := eng.CloseDay(currentDay, batchIDs); err != nil {
				return nil, err
			}
			batchIDs = nil
			for d := currentDay + 1; d < ev.BookingDay; d++ {
				if _, err := eng.CloseDay(d, nil); err != nil {
					return nil, err
				}
			}
			currentDay = ev.BookingDay
		}

		if err := eng.ApplyEvent(ev); err != nil {
			return nil, err
		}
		batchIDs = append(batchIDs, ev.ID)
	}

	for d := currentDay; d <= WindowEndDay; d++ {
		ids := batchIDs
		batchIDs = nil
		if _, err := eng.CloseDay(d, ids); err != nil {
			return nil, err
		}
	}

	return eng.DayReports(), nil
}

// RunCanonicalReplay initializes a fresh default engine and replays E1..E10 across Days 1..6.
func RunCanonicalReplay() (*Engine, []DayReport, error) {
	eng := NewDefaultEngine()
	reports, err := ReplayStream(eng, CanonicalEventStream())
	if err != nil {
		return nil, nil, err
	}
	return eng, reports, nil
}

// FormatReplayReport produces a human-readable multi-day replay audit log.
func FormatReplayReport(eng *Engine, reports []DayReport) string {
	var b strings.Builder

	b.WriteString("=== IN-MEMORY ACCOUNT LEDGER CORE: 6-DAY REPLAY REPORT ===\n")

	for _, rep := range reports {
		b.WriteString(fmt.Sprintf("\n--- DAY %d (Events ingested in cycle: %s) ---\n",
			rep.Day, joinOrNone(rep.ProcessedEventIDs)))

		b.WriteString("  Closing Balances (value_date <= day):\n")
		for _, acc := range rep.Accounts {
			line := fmt.Sprintf("    %-7s  Ledger: %-14s  Holds: %-12s  Available: %-14s  Daily Interest: %s",
				acc.AccountID,
				FormatAmount(acc.ClosingLedgerBal, acc.Currency),
				FormatAmount(acc.ActiveHolds, acc.Currency),
				FormatAmount(acc.AvailableBalance, acc.Currency),
				FormatAmount(acc.InterestAccrued, acc.Currency),
			)
			if rep.Day == WindowEndDay {
				catchUp := acc.CapitalizedInterest - sumReportedDailyAccruals(reports, acc.AccountID)
				if catchUp != 0 {
					line += fmt.Sprintf("  (incl. prior-day late-booking catch-up: %s)", FormatAmount(catchUp, acc.Currency))
				}
				line += fmt.Sprintf("  | Capitalized: %s", FormatAmount(acc.CapitalizedInterest, acc.Currency))
			}
			b.WriteString(line + "\n")
		}

		b.WriteString("  Fee Assessments:\n")
		anyFee := false
		for _, acc := range rep.Accounts {
			for _, f := range acc.FeesAssessedToday {
				anyFee = true
				b.WriteString(fmt.Sprintf("    [%s] %s for value_date Day %d (pre-fee %s -> post-fee %s, fee %s)\n",
					f.EntryID, f.AccountID, f.ValueDate,
					FormatAmount(f.PreFeeBalance, f.Currency),
					FormatAmount(f.PostFeeBal, f.Currency),
					FormatAmount(-f.FeeAmount, f.Currency),
				))
			}
		}
		if !anyFee {
			b.WriteString("    (none)\n")
		}

		b.WriteString("  Authorization States:\n")
		if len(rep.AuthorizationStates) == 0 {
			b.WriteString("    (none)\n")
		} else {
			for _, st := range rep.AuthorizationStates {
				b.WriteString(fmt.Sprintf("    %-8s (%s) status=%-8s hold=%-12s active_hold=%-12s settled=%s\n",
					st.AuthID, st.AccountID, st.Status,
					FormatAmount(st.HoldAmount, st.Currency),
					FormatAmount(st.ActiveHold, st.Currency),
					FormatAmount(st.SettledAmount, st.Currency),
				))
			}
		}

		b.WriteString("  Errors:\n")
		if len(rep.ErrorsToday) == 0 {
			b.WriteString("    (none)\n")
		} else {
			for _, errRec := range rep.ErrorsToday {
				b.WriteString(fmt.Sprintf("    [%s] event=%s account=%s: %s\n",
					errRec.Code, errRec.EventID, errRec.AccountID, errRec.Message))
			}
		}
	}

	b.WriteString("\n=== END-OF-WINDOW RECONCILED SUMMARY (Days 1..6) ===\n")
	for _, acc := range eng.Accounts() {
		b.WriteString(fmt.Sprintf("\nAccount %s (%s):\n", acc.ID, acc.Currency))
		b.WriteString("  Reconciled Closing Balance by value_date:\n")
		for d := WindowStartDay; d <= WindowEndDay; d++ {
			bal, _ := eng.LedgerBalanceByValueDate(acc.ID, d)
			b.WriteString(fmt.Sprintf("    Day %d: %s\n", d, FormatAmount(bal, acc.Currency)))
		}

		b.WriteString("  Interest Accrual Breakdown (rounded daily -> single Day 6 capitalization):\n")
		var sum Amount
		for d := WindowStartDay; d <= WindowEndDay; d++ {
			var dayAccrual Amount
			for _, a := range eng.Accruals() {
				if a.AccountID == acc.ID && a.Day == d {
					dayAccrual += a.AccruedAmount
				}
			}
			sum += dayAccrual
			b.WriteString(fmt.Sprintf("    Day %d accrual: %s\n", d, FormatAmount(dayAccrual, acc.Currency)))
		}
		b.WriteString(fmt.Sprintf("    Sum of rounded daily accruals == Capitalized total: %s\n", FormatAmount(sum, acc.Currency)))
	}

	b.WriteString("\n=== APPEND-ONLY LEDGER ENTRY LOG ===\n")
	for _, entry := range eng.Entries() {
		b.WriteString(fmt.Sprintf("  #%02d %-16s src=%-14s acc=%-7s kind=%-23s delta=%-13s bd=D%d vd=D%d inst=%d/%d\n",
			entry.Sequence, entry.EntryID, entry.SourceEventID, entry.AccountID,
			entry.Kind, FormatAmount(entry.Delta, entry.Currency),
			entry.BookingDay, entry.ValueDate, entry.InstalmentIdx, entry.InstalmentOf,
		))
	}

	return b.String()
}

func sumReportedDailyAccruals(reports []DayReport, accountID string) Amount {
	var total Amount
	for _, rep := range reports {
		for _, a := range rep.Accounts {
			if a.AccountID == accountID {
				total += a.InterestAccrued
			}
		}
	}
	return total
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
