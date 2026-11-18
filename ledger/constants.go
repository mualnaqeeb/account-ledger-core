package ledger

// Currency identifies an ISO-4217 currency code supported by the ledger core.
type Currency string

const (
	// CurrencyAED is the United Arab Emirates Dirham (2 decimal places, 100 fils).
	CurrencyAED Currency = "AED"
	// CurrencyBHD is the Bahraini Dinar (3 decimal places, 1000 fils).
	CurrencyBHD Currency = "BHD"
)

const (
	// ScaleAED is the number of decimal places for AED.
	ScaleAED int = 2
	// ScaleBHD is the number of decimal places for BHD.
	ScaleBHD int = 3

	// MinorUnitsAED is 10^ScaleAED (1 AED = 100 minor units).
	MinorUnitsAED int64 = 100
	// MinorUnitsBHD is 10^ScaleBHD (1 BHD = 1000 minor units).
	MinorUnitsBHD int64 = 1000
)

const (
	// WindowStartDay is the first business day in the replay window.
	WindowStartDay int = 1
	// WindowEndDay is the closing business day of the replay window and interest capitalization cutoff.
	WindowEndDay int = 6

	// OverdraftFeeMinorAED is the flat AED 25.00 daily overdraft fee (2500 fils).
	OverdraftFeeMinorAED int64 = 2500

	// MaxOverdraftFeesPerAccountPerDay enforces at most one overdraft fee assessment per account per value-date day.
	MaxOverdraftFeesPerAccountPerDay int = 1

	// DailyInterestRateNumerator and DailyInterestRateDenominator encode 0.04% = 0.0004 = 4 / 10,000 exact rational rate.
	DailyInterestRateNumerator   int64 = 4
	DailyInterestRateDenominator int64 = 10000

	// MinAvailableBalanceAfterHoldMinor is zero (0.00 AED / 0.000 BHD); holds leaving available balance below zero are declined.
	MinAvailableBalanceAfterHoldMinor int64 = 0

	// DefaultInstalmentCount is 1 for single-entry postings.
	DefaultInstalmentCount int = 1
	// E10InstalmentCount splits the BHD 10.000 credit in E10 across 3 immutable entries.
	E10InstalmentCount int = 3
)

const (
	// AccountID1 is the primary AED checking account in the scenario.
	AccountID1 string = "ACC-001"
	// AccountID2 is the BHD account receiving the E10 instalment credit.
	AccountID2 string = "ACC-002"
)
