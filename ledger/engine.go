package ledger

import (
	"fmt"
	"sort"
)

// Engine is an in-memory, strictly append-only, bitemporal account ledger core.
// Every state change appends immutable records; no record is ever mutated or deleted.
type Engine struct {
	accountOrder []string
	accounts     map[string]Account

	entries    []LedgerEntry
	authEvents []AuthEventRecord
	fees       []FeeAssessment
	accruals   []DailyInterestAccrual
	errors     []LedgerError
	dayReports []DayReport

	seenEventIDs map[string]bool
	closedDays   map[int]bool

	nextEntrySeq     int
	nextAuthSeq      int
	nextFeeSeq       int
	nextErrorSeq     int
	highWaterBookDay int
}

// NewEngine creates an in-memory ledger initialized with the given accounts.
func NewEngine(accounts []Account) (*Engine, error) {
	accMap := make(map[string]Account, len(accounts))
	order := make([]string, 0, len(accounts))
	for _, acc := range accounts {
		if acc.ID == "" {
			return nil, fmt.Errorf("account ID must not be empty")
		}
		if _, exists := accMap[acc.ID]; exists {
			return nil, fmt.Errorf("duplicate account ID: %s", acc.ID)
		}
		if _, err := SpecForCurrency(acc.Currency); err != nil {
			return nil, fmt.Errorf("account %s: %w", acc.ID, err)
		}
		accMap[acc.ID] = acc
		order = append(order, acc.ID)
	}
	return &Engine{
		accountOrder: order,
		accounts:     accMap,
		seenEventIDs: make(map[string]bool),
		closedDays:   make(map[int]bool),
	}, nil
}

// NewDefaultEngine creates the canonical two-account ledger (ACC-001 AED 0.00, ACC-002 BHD 0.000).
func NewDefaultEngine() *Engine {
	eng, err := NewEngine([]Account{
		{ID: AccountID1, Currency: CurrencyAED, OpeningBalance: 0},
		{ID: AccountID2, Currency: CurrencyBHD, OpeningBalance: 0},
	})
	if err != nil {
		panic(fmt.Sprintf("failed to initialize default engine: %v", err))
	}
	return eng
}

// Accounts returns a defensive copy of the registered accounts in registration order.
func (e *Engine) Accounts() []Account {
	out := make([]Account, 0, len(e.accountOrder))
	for _, id := range e.accountOrder {
		out = append(out, e.accounts[id])
	}
	return out
}

// Entries returns an immutable copy of all posted ledger entries.
func (e *Engine) Entries() []LedgerEntry {
	out := make([]LedgerEntry, len(e.entries))
	copy(out, e.entries)
	return out
}

// AuthEvents returns an immutable copy of all authorization lifecycle events.
func (e *Engine) AuthEvents() []AuthEventRecord {
	out := make([]AuthEventRecord, len(e.authEvents))
	copy(out, e.authEvents)
	return out
}

// Fees returns an immutable copy of all assessed overdraft fees.
func (e *Engine) Fees() []FeeAssessment {
	out := make([]FeeAssessment, len(e.fees))
	copy(out, e.fees)
	return out
}

// Accruals returns an immutable copy of all recorded daily interest accruals.
func (e *Engine) Accruals() []DailyInterestAccrual {
	out := make([]DailyInterestAccrual, len(e.accruals))
	copy(out, e.accruals)
	return out
}

// Errors returns an immutable copy of all rejected/declined operation errors.
func (e *Engine) Errors() []LedgerError {
	out := make([]LedgerError, len(e.errors))
	copy(out, e.errors)
	return out
}

// DayReports returns an immutable copy of all emitted end-of-day reports.
func (e *Engine) DayReports() []DayReport {
	out := make([]DayReport, len(e.dayReports))
	for i, r := range e.dayReports {
		out[i] = cloneDayReport(r)
	}
	return out
}

func cloneDayReport(r DayReport) DayReport {
	cp := DayReport{
		Day:                 r.Day,
		ProcessedEventIDs:   append([]string(nil), r.ProcessedEventIDs...),
		AuthorizationStates: append([]AuthorizationState(nil), r.AuthorizationStates...),
		ErrorsToday:         append([]LedgerError(nil), r.ErrorsToday...),
		Accounts:            make([]AccountDaySummary, len(r.Accounts)),
	}
	for i, a := range r.Accounts {
		a.FeesAssessedToday = append([]FeeAssessment(nil), a.FeesAssessedToday...)
		cp.Accounts[i] = a
	}
	return cp
}

// LedgerBalance returns the current total posted ledger balance across all entries for accountID.
func (e *Engine) LedgerBalance(accountID string) (Amount, error) {
	acc, ok := e.accounts[accountID]
	if !ok {
		return 0, fmt.Errorf("unknown account: %s", accountID)
	}
	bal := acc.OpeningBalance
	for _, entry := range e.entries {
		if entry.AccountID == accountID {
			bal += entry.Delta
		}
	}
	return bal, nil
}

// LedgerBalanceByValueDate returns the sum of opening balance and all currently posted entries
// whose ValueDate is <= maxValueDate.
func (e *Engine) LedgerBalanceByValueDate(accountID string, maxValueDate int) (Amount, error) {
	acc, ok := e.accounts[accountID]
	if !ok {
		return 0, fmt.Errorf("unknown account: %s", accountID)
	}
	bal := acc.OpeningBalance
	for _, entry := range e.entries {
		if entry.AccountID == accountID && entry.ValueDate <= maxValueDate {
			bal += entry.Delta
		}
	}
	return bal, nil
}

// LedgerBalanceForBusinessDay returns the balance of entries whose BookingDay <= day and ValueDate <= day.
// Backdated entries (ValueDate < BookingDay) do not retroactively alter prior business days' EOD interest
// bases, while late-arriving stream events belonging to BookingDay <= day are properly included.
func (e *Engine) LedgerBalanceForBusinessDay(accountID string, day int) (Amount, error) {
	acc, ok := e.accounts[accountID]
	if !ok {
		return 0, fmt.Errorf("unknown account: %s", accountID)
	}
	bal := acc.OpeningBalance
	for _, entry := range e.entries {
		if entry.AccountID == accountID && entry.BookingDay <= day && entry.ValueDate <= day {
			bal += entry.Delta
		}
	}
	return bal, nil
}

// ActiveHolds computes the sum of active authorization holds for accountID by folding the auth log.
func (e *Engine) ActiveHolds(accountID string) (Amount, error) {
	if _, ok := e.accounts[accountID]; !ok {
		return 0, fmt.Errorf("unknown account: %s", accountID)
	}
	var holds Amount
	for _, ev := range e.authEvents {
		if ev.AccountID == accountID {
			holds += ev.HoldDelta
		}
	}
	return holds, nil
}

// AvailableBalance returns LedgerBalance - ActiveHolds for accountID.
func (e *Engine) AvailableBalance(accountID string) (Amount, error) {
	ledgerBal, err := e.LedgerBalance(accountID)
	if err != nil {
		return 0, err
	}
	holds, err := e.ActiveHolds(accountID)
	if err != nil {
		return 0, err
	}
	return ledgerBal - holds, nil
}

// AuthorizationStates projects the current state of every authorization from the append-only auth log.
func (e *Engine) AuthorizationStates() []AuthorizationState {
	byID := make(map[string]AuthorizationState)
	order := make([]string, 0)
	for _, rec := range e.authEvents {
		st, exists := byID[rec.AuthID]
		if !exists {
			order = append(order, rec.AuthID)
			st = AuthorizationState{
				AuthID:     rec.AuthID,
				AccountID:  rec.AccountID,
				Currency:   rec.Currency,
				CreatedDay: rec.BookingDay,
			}
		}
		st.Status = rec.Status
		st.LastUpdated = rec.BookingDay
		st.ActiveHold += rec.HoldDelta
		if rec.Status == AuthApproved {
			st.HoldAmount = rec.HoldDelta
		}
		if rec.Status == AuthSettled {
			st.SettledAmount = rec.SettledAmount
		}
		byID[rec.AuthID] = st
	}
	sort.Strings(order)
	out := make([]AuthorizationState, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

func (e *Engine) findAuthorization(authID string) (AuthorizationState, bool) {
	for _, st := range e.AuthorizationStates() {
		if st.AuthID == authID {
			return st, true
		}
	}
	return AuthorizationState{}, false
}

// ApplyEvent validates and appends the outcome of a single InputEvent to the ledger.
// Invalid or declined operations do not mutate balances; they append an immutable error record.
func (e *Engine) ApplyEvent(ev InputEvent) error {
	if ev.BookingDay > e.highWaterBookDay {
		e.highWaterBookDay = ev.BookingDay
	}
	ingestedDay := e.highWaterBookDay
	if ingestedDay == 0 {
		ingestedDay = ev.BookingDay
	}

	if ev.BookingDay < WindowStartDay || ev.BookingDay > WindowEndDay ||
		ev.ValueDate < WindowStartDay || ev.ValueDate > WindowEndDay {
		e.recordError(ev.ID, ev.AccountID, ingestedDay, ErrInvalidDay,
			fmt.Sprintf("booking_day=%d or value_date=%d outside window [%d, %d]",
				ev.BookingDay, ev.ValueDate, WindowStartDay, WindowEndDay))
		return nil
	}

	acc, ok := e.accounts[ev.AccountID]
	if !ok {
		e.recordError(ev.ID, ev.AccountID, ingestedDay, ErrUnknownAccount,
			fmt.Sprintf("account %q does not exist", ev.AccountID))
		return nil
	}

	if ev.ID == "" || e.seenEventIDs[ev.ID] {
		e.recordError(ev.ID, ev.AccountID, ingestedDay, ErrDuplicateEventID,
			fmt.Sprintf("event ID %q is empty or already processed", ev.ID))
		return nil
	}
	e.seenEventIDs[ev.ID] = true

	switch ev.Type {
	case EventCredit:
		e.applyCredit(ev, acc, ingestedDay)
	case EventDebit:
		e.applyDebit(ev, acc, ingestedDay)
	case EventAuthorization:
		e.applyAuthorization(ev, acc, ingestedDay)
	case EventSettlement:
		e.applySettlement(ev, acc, ingestedDay)
	case EventReversal:
		e.applyReversal(ev, acc, ingestedDay)
	default:
		return fmt.Errorf("unsupported event type: %q", ev.Type)
	}
	return nil
}

func (e *Engine) applyCredit(ev InputEvent, acc Account, ingestedDay int) {
	if ev.Amount <= 0 {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrInvalidAmount, "credit amount must be positive")
		return
	}
	count := ev.InstalmentCount
	if count <= 0 {
		count = DefaultInstalmentCount
	}
	parts, err := SplitInstalments(ev.Amount, count)
	if err != nil {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrInvalidAmount, err.Error())
		return
	}
	for idx, part := range parts {
		e.appendEntry(LedgerEntry{
			EntryID:       fmt.Sprintf("%s-%d", ev.ID, idx+1),
			SourceEventID: ev.ID,
			AccountID:     acc.ID,
			Currency:      acc.Currency,
			Kind:          EntryCredit,
			Delta:         part,
			BookingDay:    ev.BookingDay,
			IngestedDay:   ingestedDay,
			ValueDate:     ev.ValueDate,
			InstalmentIdx: idx + 1,
			InstalmentOf:  count,
			Description:   ev.Description,
		})
	}
}

func (e *Engine) applyDebit(ev InputEvent, acc Account, ingestedDay int) {
	if ev.Amount <= 0 {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrInvalidAmount, "debit amount must be positive")
		return
	}
	e.appendEntry(LedgerEntry{
		EntryID:       ev.ID,
		SourceEventID: ev.ID,
		AccountID:     acc.ID,
		Currency:      acc.Currency,
		Kind:          EntryDebit,
		Delta:         -ev.Amount,
		BookingDay:    ev.BookingDay,
		IngestedDay:   ingestedDay,
		ValueDate:     ev.ValueDate,
		InstalmentIdx: 1,
		InstalmentOf:  1,
		Description:   ev.Description,
	})
}

func (e *Engine) applyAuthorization(ev InputEvent, acc Account, ingestedDay int) {
	if ev.Amount <= 0 {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrInvalidAmount, "authorization hold amount must be positive")
		return
	}
	if _, exists := e.findAuthorization(ev.AuthID); exists || ev.AuthID == "" {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrDuplicateAuthID,
			fmt.Sprintf("authorization ID %q is empty or already exists", ev.AuthID))
		return
	}
	avail, err := e.AvailableBalance(acc.ID)
	if err != nil {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrUnknownAccount, err.Error())
		return
	}
	postHoldAvail := avail - ev.Amount
	if postHoldAvail < Amount(MinAvailableBalanceAfterHoldMinor) {
		reason := fmt.Sprintf("available balance %s insufficient for hold %s (post-hold would be %s)",
			FormatAmount(avail, acc.Currency), FormatAmount(ev.Amount, acc.Currency),
			FormatAmount(postHoldAvail, acc.Currency))
		e.appendAuthEvent(AuthEventRecord{
			SourceEventID: ev.ID,
			AuthID:        ev.AuthID,
			AccountID:     acc.ID,
			Currency:      acc.Currency,
			BookingDay:    ev.BookingDay,
			ValueDate:     ev.ValueDate,
			Status:        AuthDeclined,
			HoldDelta:     0,
			Reason:        reason,
		})
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrInsufficientFunds, reason)
		return
	}
	e.appendAuthEvent(AuthEventRecord{
		SourceEventID: ev.ID,
		AuthID:        ev.AuthID,
		AccountID:     acc.ID,
		Currency:      acc.Currency,
		BookingDay:    ev.BookingDay,
		ValueDate:     ev.ValueDate,
		Status:        AuthApproved,
		HoldDelta:     ev.Amount,
		Reason:        "approved",
	})
}

func (e *Engine) applySettlement(ev InputEvent, acc Account, ingestedDay int) {
	if ev.Amount <= 0 {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrInvalidAmount, "settlement amount must be positive")
		return
	}
	st, exists := e.findAuthorization(ev.AuthID)
	if !exists {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrUnknownAuthorization,
			fmt.Sprintf("settlement rejected: authorization %q does not exist in ledger", ev.AuthID))
		return
	}
	if st.AccountID != acc.ID {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrUnknownAuthorization,
			fmt.Sprintf("settlement rejected: authorization %q belongs to %s, not %s", ev.AuthID, st.AccountID, acc.ID))
		return
	}
	if st.Status == AuthSettled {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrAuthAlreadySettled,
			fmt.Sprintf("settlement rejected: authorization %q already settled", ev.AuthID))
		return
	}
	if st.Status == AuthDeclined {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrAuthDeclined,
			fmt.Sprintf("settlement rejected: authorization %q was declined", ev.AuthID))
		return
	}

	e.appendAuthEvent(AuthEventRecord{
		SourceEventID: ev.ID,
		AuthID:        ev.AuthID,
		AccountID:     acc.ID,
		Currency:      acc.Currency,
		BookingDay:    ev.BookingDay,
		ValueDate:     ev.ValueDate,
		Status:        AuthSettled,
		HoldDelta:     -st.ActiveHold,
		SettledAmount: ev.Amount,
		Reason:        "settled",
	})
	e.appendEntry(LedgerEntry{
		EntryID:       ev.ID,
		SourceEventID: ev.ID,
		AccountID:     acc.ID,
		Currency:      acc.Currency,
		Kind:          EntrySettlement,
		Delta:         -ev.Amount,
		BookingDay:    ev.BookingDay,
		IngestedDay:   ingestedDay,
		ValueDate:     ev.ValueDate,
		AuthID:        ev.AuthID,
		InstalmentIdx: 1,
		InstalmentOf:  1,
		Description:   ev.Description,
	})
}

func (e *Engine) applyReversal(ev InputEvent, acc Account, ingestedDay int) {
	if ev.TargetEventID == "" {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrUnknownReversalRef, "reversal target event ID must not be empty")
		return
	}
	for _, existing := range e.entries {
		if existing.ReversedID == ev.TargetEventID {
			e.recordError(ev.ID, acc.ID, ingestedDay, ErrAlreadyReversed,
				fmt.Sprintf("event %q has already been reversed", ev.TargetEventID))
			return
		}
	}
	var targets []LedgerEntry
	for _, existing := range e.entries {
		if existing.SourceEventID == ev.TargetEventID && existing.AccountID == acc.ID {
			targets = append(targets, existing)
		}
	}
	if len(targets) == 0 {
		e.recordError(ev.ID, acc.ID, ingestedDay, ErrUnknownReversalRef,
			fmt.Sprintf("reversal target %q not found on account %s", ev.TargetEventID, acc.ID))
		return
	}
	for idx, orig := range targets {
		entryID := ev.ID
		if len(targets) > 1 {
			entryID = fmt.Sprintf("%s-%d", ev.ID, idx+1)
		}
		e.appendEntry(LedgerEntry{
			EntryID:       entryID,
			SourceEventID: ev.ID,
			AccountID:     acc.ID,
			Currency:      acc.Currency,
			Kind:          EntryReversal,
			Delta:         -orig.Delta,
			BookingDay:    ev.BookingDay,
			IngestedDay:   ingestedDay,
			ValueDate:     ev.ValueDate,
			ReversedID:    ev.TargetEventID,
			InstalmentIdx: idx + 1,
			InstalmentOf:  len(targets),
			Description:   ev.Description,
		})
	}
}

// CloseDay executes end-of-day processing for business day `day`:
// 1. Scans value dates [WindowStartDay..day] in ascending order and assesses at most one
//    overdraft fee per account per value-date day whenever closing balance (value_date <= d) < 0.
// 2. Computes and appends daily 0.04% interest accrual for positive balances.
// 3. On WindowEndDay (Day 6), reconciles any late-arriving business-day bookings without mutation
//    and capitalizes the exact sum of rounded daily accruals as a single credit entry per account.
func (e *Engine) CloseDay(day int, processedEventIDs []string) (DayReport, error) {
	if day < WindowStartDay || day > WindowEndDay {
		return DayReport{}, fmt.Errorf("day %d outside window [%d, %d]", day, WindowStartDay, WindowEndDay)
	}
	if e.closedDays[day] {
		return DayReport{}, fmt.Errorf("day %d already closed", day)
	}
	e.closedDays[day] = true

	feesToday := make(map[string][]FeeAssessment)
	for _, accID := range e.accountOrder {
		assessed := e.assessOverdraftFeesUpTo(accID, day)
		if len(assessed) > 0 {
			feesToday[accID] = assessed
		}
	}

	// Reconcile any late-arriving prior-day bookings (e.g. E10 BookingDay=5 arriving after E9 on Day 6)
	// by appending supplementary accrual adjustments without mutating prior accrual records.
	if day == WindowEndDay {
		for _, accID := range e.accountOrder {
			for prevDay := WindowStartDay; prevDay < day; prevDay++ {
				e.reconcilePriorDayAccrual(accID, prevDay)
			}
		}
	}

	accrualToday := make(map[string]Amount)
	capitalizedToday := make(map[string]Amount)

	for _, accID := range e.accountOrder {
		acc := e.accounts[accID]
		dayBal, _ := e.LedgerBalanceForBusinessDay(accID, day)
		dailyInt := ComputeDailyInterest(dayBal)
		e.accruals = append(e.accruals, DailyInterestAccrual{
			Day:            day,
			AccountID:      accID,
			Currency:       acc.Currency,
			ClosingBalance: dayBal,
			AccruedAmount:  dailyInt,
		})
		accrualToday[accID] = dailyInt

		if day == WindowEndDay {
			capTotal := e.sumAccrualsForAccount(accID)
			capitalizedToday[accID] = capTotal
			if capTotal > 0 {
				e.appendEntry(LedgerEntry{
					EntryID:       fmt.Sprintf("INT-CAP-D%d-%s", day, accID),
					SourceEventID: fmt.Sprintf("INT-CAP-D%d-%s", day, accID),
					AccountID:     accID,
					Currency:      acc.Currency,
					Kind:          EntryInterestCap,
					Delta:         capTotal,
					BookingDay:    day,
					IngestedDay:   day,
					ValueDate:     day,
					InstalmentIdx: 1,
					InstalmentOf:  1,
					Description:   "Day 6 interest capitalization (exact sum of rounded daily accruals)",
				})
			}
		}
	}

	summaries := make([]AccountDaySummary, 0, len(e.accountOrder))
	for _, accID := range e.accountOrder {
		acc := e.accounts[accID]
		closingBal, _ := e.LedgerBalanceByValueDate(accID, day)
		holds, _ := e.ActiveHolds(accID)
		summaries = append(summaries, AccountDaySummary{
			AccountID:           accID,
			Currency:            acc.Currency,
			ClosingLedgerBal:    closingBal,
			ActiveHolds:         holds,
			AvailableBalance:    closingBal - holds,
			FeesAssessedToday:   feesToday[accID],
			InterestAccrued:     accrualToday[accID],
			CapitalizedInterest: capitalizedToday[accID],
		})
	}

	var errsToday []LedgerError
	for _, errRec := range e.errors {
		if errRec.Day == day {
			errsToday = append(errsToday, errRec)
		}
	}

	report := DayReport{
		Day:                 day,
		ProcessedEventIDs:   append([]string(nil), processedEventIDs...),
		Accounts:            summaries,
		AuthorizationStates: e.AuthorizationStates(),
		ErrorsToday:         errsToday,
	}
	e.dayReports = append(e.dayReports, cloneDayReport(report))
	return report, nil
}

func (e *Engine) assessOverdraftFeesUpTo(accountID string, maxDay int) []FeeAssessment {
	acc := e.accounts[accountID]
	if acc.Currency != CurrencyAED {
		// Rule specifies AED 25.00 overdraft fee without an FX rate table.
		// Track non-AED accounts cleanly: no synthetic cross-currency fee is invented.
		return nil
	}
	var assessed []FeeAssessment
	for vd := WindowStartDay; vd <= maxDay; vd++ {
		if e.feeCountForAccountDay(accountID, vd) >= MaxOverdraftFeesPerAccountPerDay {
			continue
		}
		preBal, _ := e.LedgerBalanceByValueDate(accountID, vd)
		if preBal >= 0 {
			continue
		}
		feeAmt := Amount(OverdraftFeeMinorAED)
		e.nextFeeSeq++
		entryID := fmt.Sprintf("FEE-OD-%s-D%d", accountID, vd)
		e.appendEntry(LedgerEntry{
			EntryID:       entryID,
			SourceEventID: entryID,
			AccountID:     accountID,
			Currency:      acc.Currency,
			Kind:          EntryOverdraftFee,
			Delta:         -feeAmt,
			BookingDay:    maxDay,
			IngestedDay:   maxDay,
			ValueDate:     vd,
			InstalmentIdx: 1,
			InstalmentOf:  1,
			Description:   fmt.Sprintf("Overdraft fee for negative value_date Day %d closing balance", vd),
		})
		postBal := preBal - feeAmt
		rec := FeeAssessment{
			Sequence:      e.nextFeeSeq,
			AccountID:     accountID,
			Currency:      acc.Currency,
			AssessedOnDay: maxDay,
			ValueDate:     vd,
			PreFeeBalance: preBal,
			FeeAmount:     feeAmt,
			PostFeeBal:    postBal,
			EntryID:       entryID,
		}
		e.fees = append(e.fees, rec)
		assessed = append(assessed, rec)
	}
	return assessed
}

func (e *Engine) feeCountForAccountDay(accountID string, valueDate int) int {
	count := 0
	for _, f := range e.fees {
		if f.AccountID == accountID && f.ValueDate == valueDate {
			count++
		}
	}
	return count
}

func (e *Engine) reconcilePriorDayAccrual(accountID string, day int) {
	acc := e.accounts[accountID]
	currentBal, _ := e.LedgerBalanceForBusinessDay(accountID, day)
	targetAccrual := ComputeDailyInterest(currentBal)
	var recorded Amount
	for _, a := range e.accruals {
		if a.AccountID == accountID && a.Day == day {
			recorded += a.AccruedAmount
		}
	}
	diff := targetAccrual - recorded
	if diff != 0 {
		e.accruals = append(e.accruals, DailyInterestAccrual{
			Day:            day,
			AccountID:      accountID,
			Currency:       acc.Currency,
			ClosingBalance: currentBal,
			AccruedAmount:  diff,
		})
	}
}

func (e *Engine) sumAccrualsForAccount(accountID string) Amount {
	var total Amount
	for _, a := range e.accruals {
		if a.AccountID == accountID {
			total += a.AccruedAmount
		}
	}
	return total
}

func (e *Engine) appendEntry(entry LedgerEntry) {
	e.nextEntrySeq++
	entry.Sequence = e.nextEntrySeq
	e.entries = append(e.entries, entry)
}

func (e *Engine) appendAuthEvent(rec AuthEventRecord) {
	e.nextAuthSeq++
	rec.Sequence = e.nextAuthSeq
	e.authEvents = append(e.authEvents, rec)
}

func (e *Engine) recordError(eventID, accountID string, day int, code ErrorCode, msg string) {
	e.nextErrorSeq++
	e.errors = append(e.errors, LedgerError{
		Sequence:  e.nextErrorSeq,
		EventID:   eventID,
		AccountID: accountID,
		Day:       day,
		Code:      code,
		Message:   msg,
	})
}
