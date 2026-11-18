package ledger

// EventType identifies the kind of input command replayed into the ledger.
type EventType string

const (
	EventCredit        EventType = "CREDIT"
	EventDebit         EventType = "DEBIT"
	EventAuthorization EventType = "AUTHORIZATION"
	EventSettlement    EventType = "SETTLEMENT"
	EventReversal      EventType = "REVERSAL"
)

// EntryKind classifies an immutable record appended to the ledger.
type EntryKind string

const (
	EntryCredit       EntryKind = "CREDIT"
	EntryDebit        EntryKind = "DEBIT"
	EntrySettlement   EntryKind = "SETTLEMENT"
	EntryReversal     EntryKind = "REVERSAL"
	EntryOverdraftFee EntryKind = "OVERDRAFT_FEE"
	EntryInterestCap  EntryKind = "INTEREST_CAPITALIZATION"
)

// AuthStatus represents the lifecycle state of an authorization derived from the append-only auth log.
type AuthStatus string

const (
	AuthApproved AuthStatus = "APPROVED"
	AuthSettled  AuthStatus = "SETTLED"
	AuthDeclined AuthStatus = "DECLINED"
)

// ErrorCode classifies rejected operations recorded as immutable audit errors.
type ErrorCode string

const (
	ErrUnknownAccount       ErrorCode = "UNKNOWN_ACCOUNT"
	ErrInvalidAmount        ErrorCode = "INVALID_AMOUNT"
	ErrInvalidDay           ErrorCode = "INVALID_DAY"
	ErrDuplicateEventID     ErrorCode = "DUPLICATE_EVENT_ID"
	ErrDuplicateAuthID      ErrorCode = "DUPLICATE_AUTHORIZATION_ID"
	ErrUnknownAuthorization ErrorCode = "UNKNOWN_AUTHORIZATION"
	ErrAuthAlreadySettled   ErrorCode = "AUTHORIZATION_ALREADY_SETTLED"
	ErrAuthDeclined         ErrorCode = "AUTHORIZATION_DECLINED"
	ErrInsufficientFunds    ErrorCode = "INSUFFICIENT_AVAILABLE_BALANCE"
	ErrUnknownReversalRef   ErrorCode = "UNKNOWN_REVERSAL_TARGET"
	ErrAlreadyReversed      ErrorCode = "EVENT_ALREADY_REVERSED"
)

// Account defines a ledger account's static metadata and opening balance.
type Account struct {
	ID             string
	Currency       Currency
	OpeningBalance Amount
}

// InputEvent represents an inbound stream event before validation and posting.
type InputEvent struct {
	ID               string
	BookingDay       int
	ValueDate        int
	Type             EventType
	AccountID        string
	Amount           Amount // Minor units in the target account's currency
	AuthID           string // Required for AUTHORIZATION and SETTLEMENT
	TargetEventID    string // Required for REVERSAL
	InstalmentCount  int    // Defaults to 1 when <= 0
	Description      string
}

// LedgerEntry is an immutable, append-only accounting posting.
// Credits carry positive Delta; debits and fees carry negative Delta.
type LedgerEntry struct {
	Sequence      int
	EntryID       string
	SourceEventID string
	AccountID     string
	Currency      Currency
	Kind          EntryKind
	Delta         Amount
	BookingDay    int
	IngestedDay   int
	ValueDate     int
	AuthID        string
	ReversedID    string
	InstalmentIdx int
	InstalmentOf  int
	Description   string
}

// AuthEventRecord is an immutable append-only state transition for an authorization ID.
// Current authorization state is reconstructed by folding over this log without mutation.
type AuthEventRecord struct {
	Sequence      int
	SourceEventID string
	AuthID        string
	AccountID     string
	Currency      Currency
	BookingDay    int
	ValueDate     int
	Status        AuthStatus
	HoldDelta     Amount // +holdAmount on APPROVED, -holdAmount on SETTLED, 0 on DECLINED
	SettledAmount Amount // Populated on SETTLED
	Reason        string
}

// AuthorizationState is a read-only projection of an authorization's current status.
type AuthorizationState struct {
	AuthID        string
	AccountID     string
	Currency      Currency
	Status        AuthStatus
	HoldAmount    Amount
	ActiveHold    Amount
	SettledAmount Amount
	CreatedDay    int
	LastUpdated   int
}

// FeeAssessment captures an immutable daily overdraft fee decision.
type FeeAssessment struct {
	Sequence      int
	AccountID     string
	Currency      Currency
	AssessedOnDay int // Business day when EOD detected the negative value-date balance
	ValueDate     int // The day whose negative closing balance triggered the fee
	PreFeeBalance Amount
	FeeAmount     Amount
	PostFeeBal    Amount
	EntryID       string
}

// DailyInterestAccrual records a single day's calculated interest before Day 6 capitalization.
type DailyInterestAccrual struct {
	Day            int
	AccountID      string
	Currency       Currency
	ClosingBalance Amount
	AccruedAmount  Amount
}

// LedgerError is an immutable error record emitted when an event is declined or rejected.
type LedgerError struct {
	Sequence   int
	EventID    string
	AccountID  string
	Day        int
	Code       ErrorCode
	Message    string
}

// AccountDaySummary provides a per-account view inside a DayReport.
type AccountDaySummary struct {
	AccountID           string
	Currency            Currency
	ClosingLedgerBal    Amount
	ActiveHolds         Amount
	AvailableBalance    Amount
	FeesAssessedToday   []FeeAssessment
	InterestAccrued     Amount
	CapitalizedInterest Amount
}

// DayReport captures the complete immutable EOD snapshot for one business day.
type DayReport struct {
	Day                 int
	ProcessedEventIDs   []string
	Accounts            []AccountDaySummary
	AuthorizationStates []AuthorizationState
	ErrorsToday         []LedgerError
}
