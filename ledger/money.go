package ledger

import (
	"fmt"
	"strings"
)

// Amount represents a fixed-point monetary quantity in the currency's smallest minor unit (fils).
// Using int64 avoids binary floating-point representation drift completely.
type Amount int64

// CurrencySpec defines decimal scale and minor-unit factor for a currency.
type CurrencySpec struct {
	Code       Currency
	Scale      int
	MinorUnits int64
}

var currencySpecs = map[Currency]CurrencySpec{
	CurrencyAED: {Code: CurrencyAED, Scale: ScaleAED, MinorUnits: MinorUnitsAED},
	CurrencyBHD: {Code: CurrencyBHD, Scale: ScaleBHD, MinorUnits: MinorUnitsBHD},
}

// SpecForCurrency returns the precision specification for c.
func SpecForCurrency(c Currency) (CurrencySpec, error) {
	spec, ok := currencySpecs[c]
	if !ok {
		return CurrencySpec{}, fmt.Errorf("unsupported currency: %q", c)
	}
	return spec, nil
}

// FormatAmount renders an integer minor-unit amount with its exact fixed decimal scale and sign.
func FormatAmount(amount Amount, c Currency) string {
	spec, ok := currencySpecs[c]
	if !ok {
		return fmt.Sprintf("%s %d(raw)", c, amount)
	}
	sign := ""
	absVal := int64(amount)
	if absVal < 0 {
		sign = "-"
		absVal = -absVal
	}
	major := absVal / spec.MinorUnits
	minor := absVal % spec.MinorUnits
	majorStr := formatWithCommas(major)
	return fmt.Sprintf("%s %s%s.%0*d", spec.Code, sign, majorStr, spec.Scale, minor)
}

func formatWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// SplitInstalments divides total into count minor-unit slices whose sum equals total exactly.
// Any remainder r (0 <= r < count) is distributed +1 minor unit to the first r instalments,
// matching standard deterministic penny/fils allocation (e.g. 10.000 BHD / 3 -> 3.334, 3.333, 3.333).
func SplitInstalments(total Amount, count int) ([]Amount, error) {
	if count <= 0 {
		return nil, fmt.Errorf("instalment count must be positive, got %d", count)
	}
	if total < 0 {
		return nil, fmt.Errorf("instalment total must be non-negative, got %d", total)
	}
	parts := make([]Amount, count)
	base := int64(total) / int64(count)
	rem := int64(total) % int64(count)
	for i := 0; i < count; i++ {
		part := base
		if int64(i) < rem {
			part++
		}
		parts[i] = Amount(part)
	}
	return parts, nil
}

// ComputeDailyInterest computes 0.04% (DailyInterestRateNumerator / DailyInterestRateDenominator)
// on a positive closing balance in minor units, rounded to nearest minor unit using banker's
// rounding (round half to even). Non-positive balances accrue zero interest.
//
// Because balance is already stored in currency-specific minor units (10^-2 for AED, 10^-3 for BHD),
// multiplying by 4 / 10,000 and rounding to integer minor units produces the exact rounded daily
// accrual in the currency's native scale.
func ComputeDailyInterest(closingBalance Amount) Amount {
	if closingBalance <= 0 {
		return 0
	}
	num := int64(closingBalance) * DailyInterestRateNumerator
	den := DailyInterestRateDenominator
	q := num / den
	r := num % den
	twiceR := r * 2
	if twiceR > den {
		q++
	} else if twiceR == den && q%2 != 0 {
		q++
	}
	return Amount(q)
}
