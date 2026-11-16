// Package money provides Money: an amount of one currency.
//
// An amount is kept exactly, to 9 decimal places, whatever the currency. That
// is finer than the smallest unit of most currencies, so that a price per
// item, a fee or an average does not lose its fraction. Round brings an
// amount back to what the currency can pay.
//
// Amounts of different currencies never mix: an operation on two of them
// returns ErrCurrencyMismatch. No operation uses binary floating point.
//
// Money is written as text ("12.50 USD") and as a JSON object. Reading it
// back needs an ISO 4217 currency, because a code alone does not give the
// decimal places of any other.
//
// In a database an amount takes two columns, a number and a currency code:
// write Number and Currency, and read them back for NewMoneyFromString. Money
// is deliberately not a driver.Valuer or sql.Scanner, which can only fill one
// column.
package money

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/talav/currency"
)

const (
	// nanosPerUnit is how many nanos, the billionths an amount is counted
	// in, make one unit of a currency.
	nanosPerUnit = 1_000_000_000

	// maxDecimals is the number of decimal places an amount keeps.
	maxDecimals = 9
)

var (
	// ErrCurrencyMismatch is returned by an operation on two amounts of
	// different currencies.
	ErrCurrencyMismatch = errors.New("currencies differ")

	// ErrOverflow is returned when an amount is too large to hold: its whole
	// part must fit in 63 bits.
	ErrOverflow = errors.New("amount out of range")
)

// Money is an amount of one currency, kept exactly to 9 decimal places. The
// amount may be negative.
//
// Money comes only from a constructor or from an operation on other Money,
// so it always has a currency and an amount in range. Money values are
// comparable with == and usable as map keys; two are equal when they are the
// same amount of the same currency.
//
// The zero value is the one exception: it has no currency, so it is not
// money. It stands for a missing amount, IsValid reports false for it, and
// other methods are meaningless on it. Nothing of a currency is a different,
// valid value.
type Money struct {
	// units is the whole part of the amount, in units of the currency. It
	// is never math.MinInt64, so that every amount can be negated.
	units int64

	// nanos is the fraction of the amount in billionths of a unit,
	// -999,999,999 to 999,999,999. It is zero or has the sign of units,
	// unless units is zero.
	nanos int64

	// currency is the currency of the amount. It is never the zero Currency
	// and has at most 9 decimal places.
	currency currency.Currency
}

// NewMoney returns the amount of c with the given whole units and nanos,
// billionths of a unit: 12.50 is 12 units and 500,000,000 nanos, and -0.25
// is 0 units and -250,000,000 nanos.
//
// It returns an error if c is the zero Currency or has more than 9 decimal
// places, if nanos is not within one unit, if units and nanos have opposite
// signs, or if units is math.MinInt64.
func NewMoney(units, nanos int64, c currency.Currency) (Money, error) {
	m := Money{units: units, nanos: nanos, currency: c}
	if err := m.validate(); err != nil {
		return Money{}, err
	}

	return m, nil
}

// NewMoneyFromMinor returns the amount of c that is the given number of its
// smallest units: 1250 is 12.50 of a currency with two decimal places, and
// 1250 of one with none.
//
// It returns an error if c is the zero Currency or has more than 9 decimal
// places, or if the amount is out of range.
func NewMoneyFromMinor(minor int64, c currency.Currency) (Money, error) {
	zero := Money{currency: c}
	if err := zero.validate(); err != nil {
		return Money{}, err
	}

	total := big.NewInt(minor)

	m, err := newMoneyFromTotal(total.Mul(total, zero.minorStep()), c)
	if err != nil {
		return Money{}, fmt.Errorf("%d minor units of %s: %w", minor, c, err)
	}

	return m, nil
}

// NewMoneyFromString returns the amount of c written in s as a decimal
// number: digits, then optionally a point and one to nine more digits, with
// an optional minus sign in front, such as 12.50, -0.0035 or 100. White
// space around the number is ignored.
//
// It returns an error if c is the zero Currency or has more than 9 decimal
// places, if s is not such a number, or if the amount is out of range. A
// number with more decimal places than the currency has is accepted and
// kept; Round removes them.
func NewMoneyFromString(s string, c currency.Currency) (Money, error) {
	zero := Money{currency: c}
	if err := zero.validate(); err != nil {
		return Money{}, err
	}

	total, ok := parseTotal(strings.TrimSpace(s))
	if !ok {
		return Money{}, fmt.Errorf("invalid amount %q: must be a decimal number with at most %d decimal places", s, maxDecimals)
	}

	m, err := newMoneyFromTotal(total, c)
	if err != nil {
		return Money{}, fmt.Errorf("invalid amount %q: %w", s, err)
	}

	return m, nil
}

// IsValid reports whether m is money. It is false only for the zero value,
// which has no currency and stands for a missing amount; all Money from a
// constructor or an operation is valid. It says nothing about the size of
// the amount: IsZero tests for nothing of a currency.
func (m Money) IsValid() bool {
	return m.currency.IsValid()
}

// Currency returns the currency of the amount.
func (m Money) Currency() currency.Currency { return m.currency }

// Units returns the whole part of the amount: 12 for 12.50, -12 for -12.50
// and 0 for -0.25.
func (m Money) Units() int64 { return m.units }

// Nanos returns the fraction of the amount in billionths of a unit:
// 500,000,000 for 12.50 and -250,000,000 for -0.25. It is zero or has the
// sign of the amount.
func (m Money) Nanos() int64 { return m.nanos }

// Minor returns the amount as a number of the smallest units of its
// currency: 1250 for 12.50 of a currency with two decimal places.
//
// It returns an error if the amount is not a whole number of them, as 0.005
// is not for such a currency; Round first. It returns ErrOverflow if the
// number does not fit.
func (m Money) Minor() (int64, error) {
	minor, remainder := new(big.Int).QuoRem(m.total(), m.minorStep(), new(big.Int))
	if remainder.Sign() != 0 {
		return 0, fmt.Errorf("%s is not a whole number of minor units", m)
	}

	if !minor.IsInt64() {
		return 0, fmt.Errorf("minor units of %s: %w", m, ErrOverflow)
	}

	return minor.Int64(), nil
}

// Sign returns -1 for a negative amount, 0 for nothing and 1 for a positive
// amount.
func (m Money) Sign() int {
	return cmp.Or(cmp.Compare(m.units, 0), cmp.Compare(m.nanos, 0))
}

// IsZero reports whether the amount is nothing: zero of its currency. It is
// not a test for the zero value, which has no currency; IsValid is.
func (m Money) IsZero() bool { return m.Sign() == 0 }

// IsPositive reports whether the amount is greater than nothing.
func (m Money) IsPositive() bool { return m.Sign() > 0 }

// IsNegative reports whether the amount is less than nothing.
func (m Money) IsNegative() bool { return m.Sign() < 0 }

// SameCurrency reports whether m and m2 are amounts of the same currency.
func (m Money) SameCurrency(m2 Money) bool {
	return m.currency == m2.currency
}

// Compare orders two amounts of one currency for sorting, as cmp.Compare
// does: negative when m is less than m2, positive when it is greater, zero
// when they are equal. It returns ErrCurrencyMismatch for two currencies.
func (m Money) Compare(m2 Money) (int, error) {
	if err := m.checkSameCurrency(m2); err != nil {
		return 0, err
	}

	return cmp.Or(cmp.Compare(m.units, m2.units), cmp.Compare(m.nanos, m2.nanos)), nil
}

// Min returns the lesser of m and m2. It returns ErrCurrencyMismatch for two
// currencies.
func (m Money) Min(m2 Money) (Money, error) {
	order, err := m.Compare(m2)
	if err != nil {
		return Money{}, err
	}

	if order > 0 {
		return m2, nil
	}

	return m, nil
}

// Max returns the greater of m and m2. It returns ErrCurrencyMismatch for
// two currencies.
func (m Money) Max(m2 Money) (Money, error) {
	order, err := m.Compare(m2)
	if err != nil {
		return Money{}, err
	}

	if order < 0 {
		return m2, nil
	}

	return m, nil
}

// Neg returns the amount with its sign changed: a debit for a credit.
func (m Money) Neg() Money {
	return Money{units: -m.units, nanos: -m.nanos, currency: m.currency}
}

// Abs returns the amount without its sign.
func (m Money) Abs() Money {
	if m.IsNegative() {
		return m.Neg()
	}

	return m
}

// Add returns m plus m2. It returns ErrCurrencyMismatch for two currencies
// and ErrOverflow if the sum is out of range.
func (m Money) Add(m2 Money) (Money, error) {
	if err := m.checkSameCurrency(m2); err != nil {
		return Money{}, err
	}

	// The nanos of two amounts add up to less than two units, so they carry
	// at most one unit either way.
	nanos := m.nanos + m2.nanos
	units, fits := addInt64(m.units, m2.units)
	units, carryFits := addInt64(units, nanos/nanosPerUnit)
	nanos %= nanosPerUnit

	// Amounts of opposite signs leave parts of opposite signs: borrow one
	// unit so that both parts carry the sign of the sum.
	switch {
	case units > 0 && nanos < 0:
		units--
		nanos += nanosPerUnit
	case units < 0 && nanos > 0:
		units++
		nanos -= nanosPerUnit
	}

	if !fits || !carryFits || units == math.MinInt64 {
		return Money{}, fmt.Errorf("%s plus %s: %w", m, m2, ErrOverflow)
	}

	return Money{units: units, nanos: nanos, currency: m.currency}, nil
}

// Sub returns m minus m2. It returns ErrCurrencyMismatch for two currencies
// and ErrOverflow if the difference is out of range.
func (m Money) Sub(m2 Money) (Money, error) {
	return m.Add(m2.Neg())
}

// Mul returns the amount n times over: the cost of n items at m each. It
// returns ErrOverflow if the product is out of range.
func (m Money) Mul(n int64) (Money, error) {
	total := m.total()

	product, err := newMoneyFromTotal(total.Mul(total, big.NewInt(n)), m.currency)
	if err != nil {
		return Money{}, fmt.Errorf("%s times %d: %w", m, n, err)
	}

	return product, nil
}

// MulRatio returns the amount multiplied by numerator/denominator, rounded
// to 9 decimal places by mode. A ratio states a factor exactly: 8.25% is
// 825/10000, and a third is 1/3.
//
// It returns an error if the denominator is zero or the mode is unknown, and
// ErrOverflow if the result is out of range.
func (m Money) MulRatio(numerator, denominator int64, mode RoundingMode) (Money, error) {
	if denominator == 0 {
		return Money{}, errors.New("ratio denominator must not be zero")
	}

	total := m.total()

	scaled, err := divRound(total.Mul(total, big.NewInt(numerator)), big.NewInt(denominator), mode)
	if err != nil {
		return Money{}, err
	}

	product, err := newMoneyFromTotal(scaled, m.currency)
	if err != nil {
		return Money{}, fmt.Errorf("%s times %d/%d: %w", m, numerator, denominator, err)
	}

	return product, nil
}

// Div returns the amount divided by n, rounded to 9 decimal places by mode:
// the price of one of n items that cost m together. The parts need not add
// up to m again; Split divides an amount without losing any of it.
//
// It returns an error if n is zero or the mode is unknown.
func (m Money) Div(n int64, mode RoundingMode) (Money, error) {
	if n == 0 {
		return Money{}, errors.New("division by zero")
	}

	return m.MulRatio(1, n, mode)
}

// Round returns the amount rounded by mode to the decimal places of its
// currency: to cents for a currency with two.
//
// It returns an error if the mode is unknown, and ErrOverflow if rounding
// up takes the amount out of range.
func (m Money) Round(mode RoundingMode) (Money, error) {
	return m.RoundTo(m.currency.Decimals(), mode)
}

// RoundTo returns the amount rounded by mode to the given number of decimal
// places, 0 to 9.
//
// It returns an error if decimals is out of that range or the mode is
// unknown, and ErrOverflow if rounding up takes the amount out of range.
func (m Money) RoundTo(decimals int, mode RoundingMode) (Money, error) {
	if decimals < 0 || decimals > maxDecimals {
		return Money{}, fmt.Errorf("decimals %d must be in 0..%d", decimals, maxDecimals)
	}

	return m.roundToStep(big.NewInt(pow10(maxDecimals-decimals)), mode)
}

// RoundToMultiple returns the amount rounded by mode to a whole number of
// steps: to 0.05 where the smallest coin is five cents, or to the tick a
// price moves in.
//
// It returns ErrCurrencyMismatch if step is of another currency, an error if
// step is not positive or the mode is unknown, and ErrOverflow if rounding
// up takes the amount out of range.
func (m Money) RoundToMultiple(step Money, mode RoundingMode) (Money, error) {
	if err := m.checkSameCurrency(step); err != nil {
		return Money{}, err
	}

	if !step.IsPositive() {
		return Money{}, fmt.Errorf("step %s must be positive", step)
	}

	return m.roundToStep(step.total(), mode)
}

// Allocate divides the amount among parties in proportion to ratios and
// returns one share for each, in the order of the ratios. The shares always
// add up to the amount: what the proportions leave over is handed out one
// smallest unit at a time, starting with the first party. A party with a
// ratio of zero gets nothing.
//
// The smallest unit is that of the currency, a cent for one with two decimal
// places. An amount finer than that is divided to the last of its 9 decimal
// places instead.
//
// It returns an error if there are no ratios, if one is negative, or if all
// are zero.
func (m Money) Allocate(ratios ...int64) ([]Money, error) {
	if len(ratios) == 0 {
		return nil, errors.New("no ratios to allocate by")
	}

	sum := new(big.Int)

	for _, ratio := range ratios {
		if ratio < 0 {
			return nil, fmt.Errorf("ratio %d must not be negative", ratio)
		}

		sum.Add(sum, big.NewInt(ratio))
	}

	if sum.Sign() == 0 {
		return nil, errors.New("ratios must not all be zero")
	}

	// Work on a count of smallest units without its sign, so that a debt is
	// shared out exactly as a credit of the same size is.
	step := m.allocationStep()
	left := m.total()
	left.Abs(left).Quo(left, step)

	counts := make([]*big.Int, len(ratios))
	for i, ratio := range ratios {
		counts[i] = new(big.Int).Mul(left, big.NewInt(ratio))
		counts[i].Quo(counts[i], sum)
	}

	for _, count := range counts {
		left.Sub(left, count)
	}

	// Fewer units are left over than there are parties with a ratio, so one
	// pass hands them all out.
	shares := make([]Money, len(ratios))

	for i, count := range counts {
		if left.Sign() > 0 && ratios[i] > 0 {
			count.Add(count, big.NewInt(1))
			left.Sub(left, big.NewInt(1))
		}

		shares[i] = m.shareOf(count.Mul(count, step))
	}

	return shares, nil
}

// Split divides the amount into the given number of shares that are as
// equal as its smallest unit allows and add up to the amount. The shares
// that come first are one smallest unit larger when the amount does not
// divide evenly. The smallest unit is the one Allocate uses.
//
// It returns an error if parts is less than one.
func (m Money) Split(parts int) ([]Money, error) {
	if parts < 1 {
		return nil, fmt.Errorf("parts %d must be at least 1", parts)
	}

	ratios := make([]int64, parts)
	for i := range ratios {
		ratios[i] = 1
	}

	return m.Allocate(ratios...)
}

// Convert returns the amount in the currency to, at the rate
// numerator/denominator units of to for one unit of the amount's currency,
// rounded to 9 decimal places by mode. A rate of 1.0853 is 10853/10000.
//
// The result keeps its 9 decimal places; Round brings it to those of the
// new currency.
//
// It returns an error if to is the zero Currency or has more than 9 decimal
// places, if the rate is not positive, or if the mode is unknown, and
// ErrOverflow if the result is out of range.
func (m Money) Convert(to currency.Currency, numerator, denominator int64, mode RoundingMode) (Money, error) {
	zero := Money{currency: to}
	if err := zero.validate(); err != nil {
		return Money{}, err
	}

	if numerator <= 0 || denominator <= 0 {
		return Money{}, fmt.Errorf("rate %d/%d must be positive", numerator, denominator)
	}

	converted, err := m.MulRatio(numerator, denominator, mode)
	if err != nil {
		return Money{}, err
	}

	converted.currency = to

	return converted, nil
}

// Number returns the amount as a plain decimal number without its currency,
// such as "12.50". It shows the decimal places of the currency, and more
// when the amount is finer: "0.0035". NewMoneyFromString reads it back, and
// it is what goes in the number column of a database.
func (m Money) Number() string {
	return string(m.appendNumber(make([]byte, 0, len("-9223372036854775807.999999999"))))
}

// String returns the amount as Number writes it, a space and the currency
// code, such as "12.50 USD".
func (m Money) String() string {
	b := m.appendNumber(make([]byte, 0, len("-9223372036854775807.999999999 ")+len(m.currency.Code())))
	b = append(b, ' ')

	return string(append(b, m.currency.Code()...))
}

// MarshalText returns the String form of the amount, such as "12.50 USD". It
// never fails. It makes Money an encoding.TextMarshaler, which encoding/json
// uses for map keys.
func (m Money) MarshalText() ([]byte, error) {
	return []byte(m.String()), nil
}

// UnmarshalText sets m to the amount written in text as a number and an
// ISO 4217 currency code with white space between them, such as "12.50 USD".
// The number is read as NewMoneyFromString reads one. If the text is not
// such an amount, it returns the error and leaves m as it was. It makes
// *Money an encoding.TextUnmarshaler.
//
// A code alone does not say how many decimal places a currency outside
// ISO 4217 has, so an amount of one cannot be read back.
func (m *Money) UnmarshalText(text []byte) error {
	parsed, err := parseMoney(string(text))
	if err != nil {
		return err
	}

	*m = parsed

	return nil
}

// MarshalJSON returns the amount as a JSON object holding the number as a
// string and the currency code: {"amount":"12.50","currency":"USD"}. The
// number is a string so that no reader turns it into a binary fraction. It
// makes Money a json.Marshaler.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(moneyJSON{Amount: m.Number(), Currency: m.currency})
}

// UnmarshalJSON sets m from a JSON object as MarshalJSON writes it. The
// amount must be a JSON string and the currency an ISO 4217 code. On error m
// is unchanged. A JSON null leaves m as it was, as encoding/json does for
// its own types. It makes *Money a json.Unmarshaler.
func (m *Money) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var wire moneyJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("invalid money: %w", err)
	}

	parsed, err := NewMoneyFromString(wire.Amount, wire.Currency)
	if err != nil {
		return fmt.Errorf("invalid money: %w", err)
	}

	*m = parsed

	return nil
}

func (m Money) validate() error {
	if !m.currency.IsValid() {
		return errors.New("currency is missing")
	}

	if m.currency.Decimals() > maxDecimals {
		return fmt.Errorf("currency %s has %d decimal places: money keeps at most %d", m.currency, m.currency.Decimals(), maxDecimals)
	}

	if m.units == math.MinInt64 {
		return fmt.Errorf("units %d: %w", m.units, ErrOverflow)
	}

	if m.nanos <= -nanosPerUnit || m.nanos >= nanosPerUnit {
		return fmt.Errorf("nanos %d must be in -%d..%d", m.nanos, nanosPerUnit-1, nanosPerUnit-1)
	}

	if (m.units > 0 && m.nanos < 0) || (m.units < 0 && m.nanos > 0) {
		return fmt.Errorf("units %d and nanos %d must have the same sign", m.units, m.nanos)
	}

	return nil
}

// checkSameCurrency returns ErrCurrencyMismatch unless m and m2 are amounts
// of one currency.
func (m Money) checkSameCurrency(m2 Money) error {
	if m.currency != m2.currency {
		return fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.currency, m2.currency)
	}

	return nil
}

// appendNumber appends the amount as a plain decimal number to b: at least
// the decimal places of the currency, and as many more as the amount needs.
func (m Money) appendNumber(b []byte) []byte {
	if m.IsNegative() {
		b = append(b, '-')
	}

	abs := m.Abs()
	b = strconv.AppendInt(b, abs.units, 10)

	decimals := m.currency.Decimals()
	for scale := decimals; scale < maxDecimals; scale++ {
		if abs.nanos%pow10(maxDecimals-scale) == 0 {
			break
		}

		decimals = scale + 1
	}

	if decimals == 0 {
		return b
	}

	fraction := strconv.FormatInt(abs.nanos/pow10(maxDecimals-decimals), 10)

	b = append(b, '.')
	b = append(b, strings.Repeat("0", decimals-len(fraction))...)

	return append(b, fraction...)
}

// total returns the whole amount in nanos. Operations that multiply or
// divide work on this number, which does not fit in 64 bits.
func (m Money) total() *big.Int {
	total := big.NewInt(m.units)
	total.Mul(total, big.NewInt(nanosPerUnit))

	return total.Add(total, big.NewInt(m.nanos))
}

// minorStep returns the smallest unit of the amount's currency in nanos.
func (m Money) minorStep() *big.Int {
	return big.NewInt(pow10(maxDecimals - m.currency.Decimals()))
}

// allocationStep returns the unit, in nanos, that Allocate hands out: the
// smallest unit of the currency when the amount is a whole number of them,
// and one nano when it is finer.
func (m Money) allocationStep() *big.Int {
	step := m.minorStep()
	if new(big.Int).Rem(m.total(), step).Sign() != 0 {
		return big.NewInt(1)
	}

	return step
}

// roundToStep returns the amount rounded by mode to a whole number of steps
// of the given size in nanos.
func (m Money) roundToStep(step *big.Int, mode RoundingMode) (Money, error) {
	steps, err := divRound(m.total(), step, mode)
	if err != nil {
		return Money{}, err
	}

	rounded, err := newMoneyFromTotal(steps.Mul(steps, step), m.currency)
	if err != nil {
		return Money{}, fmt.Errorf("round %s: %w", m, err)
	}

	return rounded, nil
}

// shareOf returns a share of the amount that is the given number of nanos
// without a sign. It takes the sign and the currency of the amount. A share
// is never larger than the amount, so it is always in range.
func (m Money) shareOf(nanos *big.Int) Money {
	units, fraction := new(big.Int).QuoRem(nanos, big.NewInt(nanosPerUnit), new(big.Int))
	share := Money{units: units.Int64(), nanos: fraction.Int64(), currency: m.currency}

	if m.IsNegative() {
		return share.Neg()
	}

	return share
}

// moneyJSON is the JSON form of Money.
type moneyJSON struct {
	// Amount is the amount as a plain decimal number.
	Amount string `json:"amount"`

	// Currency is the currency of the amount, written as its code.
	Currency currency.Currency `json:"currency"`
}

// parseMoney reads an amount written as a number and an ISO 4217 currency
// code with white space between them.
func parseMoney(text string) (Money, error) {
	fields := strings.Fields(text)
	if len(fields) != 2 {
		return Money{}, fmt.Errorf("invalid money %q: must be a number and a currency code, such as 12.50 USD", text)
	}

	c, err := currency.NewCurrencyFromString(fields[1])
	if err != nil {
		return Money{}, fmt.Errorf("invalid money %q: %w", text, err)
	}

	m, err := NewMoneyFromString(fields[0], c)
	if err != nil {
		return Money{}, fmt.Errorf("invalid money %q: %w", text, err)
	}

	return m, nil
}

// newMoneyFromTotal returns the amount of c that is the given number of
// nanos. It returns ErrOverflow if the amount is out of range. The currency
// must already be known to be valid.
func newMoneyFromTotal(total *big.Int, c currency.Currency) (Money, error) {
	units, nanos := new(big.Int).QuoRem(total, big.NewInt(nanosPerUnit), new(big.Int))
	if !units.IsInt64() || units.Int64() == math.MinInt64 {
		return Money{}, ErrOverflow
	}

	return Money{units: units.Int64(), nanos: nanos.Int64(), currency: c}, nil
}

// addInt64 returns a plus b and whether the sum fits in 64 bits. It cannot
// fit only when both have one sign and the sum comes out with the other.
func addInt64(a, b int64) (int64, bool) {
	sum := a + b

	return sum, (a < 0) != (b < 0) || (sum < 0) == (a < 0)
}

// parseTotal reads a decimal number as a count of nanos. It reports false if
// s is not digits, optionally followed by a point and one to nine digits,
// with an optional minus sign in front.
func parseTotal(s string) (*big.Int, bool) {
	digits, negative := strings.CutPrefix(s, "-")
	whole, fraction, hasPoint := strings.Cut(digits, ".")

	if !isDigits(whole) || hasPoint != isDigits(fraction) || len(fraction) > maxDecimals {
		return nil, false
	}

	// Digits alone cannot fail to parse. Pad the fraction to nine digits so
	// that the number read is already in nanos.
	total, _ := new(big.Int).SetString(whole+fraction+strings.Repeat("0", maxDecimals-len(fraction)), 10)
	if negative {
		total.Neg(total)
	}

	return total, true
}

// isDigits reports whether s is one or more ASCII digits.
func isDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return s != ""
}

// pow10 returns ten to the power of exponent, which must be in 0 to 9.
func pow10(exponent int) int64 {
	power := int64(1)
	for range exponent {
		power *= 10
	}

	return power
}
