// Package money provides Money: an amount of one currency.
//
// An amount is kept exactly, to 18 decimal places, whatever the currency. That
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
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/talav/currency"
	"github.com/talav/fixed"
)

// ErrCurrencyMismatch is returned by an operation on two amounts of
// different currencies.
var ErrCurrencyMismatch = errors.New("currencies differ")

// Money is an amount of one currency, kept exactly to 18 decimal places. The
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
	// amount is the number of units of the currency.
	amount fixed.Decimal

	// currency is the currency of the amount. It is never the zero Currency.
	currency currency.Currency
}

// NewMoney returns the amount of c that is the given number of its units:
// 12.5 is 12.50 of a currency with two decimal places.
//
// It returns an error if c is the zero Currency.
func NewMoney(amount fixed.Decimal, c currency.Currency) (Money, error) {
	m := Money{amount: amount, currency: c}
	if err := m.validate(); err != nil {
		return Money{}, err
	}

	return m, nil
}

// NewMoneyFromMinor returns the amount of c that is the given number of its
// smallest units: 1250 is 12.50 of a currency with two decimal places, and
// 1250 of one with none.
//
// It returns an error if c is the zero Currency, or if the amount is out of
// range.
func NewMoneyFromMinor(minor int64, c currency.Currency) (Money, error) {
	amount, err := fixed.NewDecimalFromInt64(minor, c.Decimals())
	if err != nil {
		return Money{}, fmt.Errorf("%d minor units of %s: %w", minor, c, err)
	}

	return NewMoney(amount, c)
}

// NewMoneyFromString returns the amount of c written in s as a decimal
// number: digits, then optionally a point and 1 to 18 more digits, with an
// optional minus sign in front, such as 12.50, -0.0035 or 100. White space
// around the number is ignored.
//
// It returns an error if c is the zero Currency, if s is not such a number,
// or if the amount is out of range. A number with more decimal places than
// the currency has is accepted and kept; Round removes them.
func NewMoneyFromString(s string, c currency.Currency) (Money, error) {
	amount, err := fixed.Parse(strings.TrimSpace(s))
	if err != nil {
		return Money{}, err
	}

	return NewMoney(amount, c)
}

// IsValid reports whether m is money. It is false only for the zero value,
// which has no currency and stands for a missing amount; all Money from a
// constructor or an operation is valid. It says nothing about the size of
// the amount: IsZero tests for nothing of a currency.
func (m Money) IsValid() bool {
	return m.currency.IsValid()
}

// Amount returns the amount as a number, without its currency.
func (m Money) Amount() fixed.Decimal { return m.amount }

// Currency returns the currency of the amount.
func (m Money) Currency() currency.Currency { return m.currency }

// Minor returns the amount as a number of the smallest units of its
// currency: 1250 for 12.50 of a currency with two decimal places.
//
// It returns an error if the amount is not a whole number of them, as 0.005
// is not for such a currency; Round first. It returns fixed.ErrOverflow if
// the number does not fit.
func (m Money) Minor() (int64, error) {
	minor, err := m.amount.Int64(m.currency.Decimals())
	if err != nil {
		return 0, fmt.Errorf("minor units of %s: %w", m, err)
	}

	return minor, nil
}

// Sign returns -1 for a negative amount, 0 for nothing and 1 for a positive
// amount.
func (m Money) Sign() int {
	return m.amount.Sign()
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

	return m.amount.Compare(m2.amount), nil
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
	return Money{amount: m.amount.Neg(), currency: m.currency}
}

// Abs returns the amount without its sign.
func (m Money) Abs() Money {
	if m.IsNegative() {
		return m.Neg()
	}

	return m
}

// Add returns m plus m2. It returns ErrCurrencyMismatch for two currencies
// and fixed.ErrOverflow if the sum is out of range.
func (m Money) Add(m2 Money) (Money, error) {
	if err := m.checkSameCurrency(m2); err != nil {
		return Money{}, err
	}

	amount, err := m.amount.Add(m2.amount)
	if err != nil {
		return Money{}, err
	}

	return Money{amount: amount, currency: m.currency}, nil
}

// Sub returns m minus m2. It returns ErrCurrencyMismatch for two currencies
// and fixed.ErrOverflow if the difference is out of range.
func (m Money) Sub(m2 Money) (Money, error) {
	if err := m.checkSameCurrency(m2); err != nil {
		return Money{}, err
	}

	amount, err := m.amount.Sub(m2.amount)
	if err != nil {
		return Money{}, err
	}

	return Money{amount: amount, currency: m.currency}, nil
}

// Mul returns the amount n times over: the cost of n items at m each. It
// returns fixed.ErrOverflow if the product is out of range.
func (m Money) Mul(n int64) (Money, error) {
	amount, err := m.amount.MulInt(n)
	if err != nil {
		return Money{}, err
	}

	return Money{amount: amount, currency: m.currency}, nil
}

// MulRatio returns the amount multiplied by numerator/denominator, rounded
// to 18 decimal places by mode. A ratio states a factor exactly: 8.25% is
// 825/10000, and a third is 1/3.
//
// It returns an error if the denominator is zero or the mode is unknown, and
// fixed.ErrOverflow if the result is out of range.
func (m Money) MulRatio(numerator, denominator int64, mode fixed.RoundingMode) (Money, error) {
	amount, err := m.amount.MulRatio(numerator, denominator, mode)
	if err != nil {
		return Money{}, err
	}

	return Money{amount: amount, currency: m.currency}, nil
}

// Div returns the amount divided by n, rounded to 18 decimal places by mode:
// the price of one of n items that cost m together. The parts need not add
// up to m again; Split divides an amount without losing any of it.
//
// It returns an error if n is zero or the mode is unknown.
func (m Money) Div(n int64, mode fixed.RoundingMode) (Money, error) {
	if n == 0 {
		return Money{}, errors.New("division by zero")
	}

	return m.MulRatio(1, n, mode)
}

// Round returns the amount rounded by mode to the decimal places of its
// currency: to cents for a currency with two.
//
// It returns an error if the mode is unknown, and fixed.ErrOverflow if
// rounding up takes the amount out of range.
func (m Money) Round(mode fixed.RoundingMode) (Money, error) {
	return m.RoundTo(m.currency.Decimals(), mode)
}

// RoundTo returns the amount rounded by mode to the given number of decimal
// places, 0 to 18.
//
// It returns an error if decimals is out of that range or the mode is
// unknown, and fixed.ErrOverflow if rounding up takes the amount out of
// range.
func (m Money) RoundTo(decimals int, mode fixed.RoundingMode) (Money, error) {
	amount, err := m.amount.Round(decimals, mode)
	if err != nil {
		return Money{}, err
	}

	return Money{amount: amount, currency: m.currency}, nil
}

// RoundToMultiple returns the amount rounded by mode to a whole number of
// steps: to 0.05 where the smallest coin is five cents, or to the tick a
// price moves in.
//
// It returns ErrCurrencyMismatch if step is of another currency, an error if
// step is not positive or the mode is unknown, and fixed.ErrOverflow if
// rounding up takes the amount out of range.
func (m Money) RoundToMultiple(step Money, mode fixed.RoundingMode) (Money, error) {
	if err := m.checkSameCurrency(step); err != nil {
		return Money{}, err
	}

	amount, err := m.amount.RoundToMultiple(step.amount, mode)
	if err != nil {
		return Money{}, err
	}

	return Money{amount: amount, currency: m.currency}, nil
}

// Allocate divides the amount among parties in proportion to ratios and
// returns one share for each, in the order of the ratios. The shares always
// add up to the amount: what the proportions leave over is handed out one
// smallest unit at a time, starting with the first party. A party with a
// ratio of zero gets nothing.
//
// The smallest unit is that of the currency, a cent for one with two decimal
// places. An amount finer than that is divided to the last of its 18 decimal
// places instead.
//
// It returns an error if there are no ratios, if one is negative, or if all
// are zero.
func (m Money) Allocate(ratios ...int64) ([]Money, error) {
	amounts, err := m.amount.Allocate(m.allocationStep(), ratios...)
	if err != nil {
		return nil, err
	}

	shares := make([]Money, len(amounts))
	for i, amount := range amounts {
		shares[i] = Money{amount: amount, currency: m.currency}
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
// rounded to 18 decimal places by mode. A rate of 1.0853 is 10853/10000.
//
// The result keeps its 18 decimal places; Round brings it to those of the
// new currency.
//
// It returns an error if to is the zero Currency, if the rate is not
// positive, or if the mode is unknown, and fixed.ErrOverflow if the result
// is out of range.
func (m Money) Convert(to currency.Currency, numerator, denominator int64, mode fixed.RoundingMode) (Money, error) {
	if numerator <= 0 || denominator <= 0 {
		return Money{}, fmt.Errorf("rate %d/%d must be positive", numerator, denominator)
	}

	amount, err := m.amount.MulRatio(numerator, denominator, mode)
	if err != nil {
		return Money{}, err
	}

	return NewMoney(amount, to)
}

// Number returns the amount as a plain decimal number without its currency,
// such as "12.50". It shows the decimal places of the currency, and more
// when the amount is finer: "0.0035". NewMoneyFromString reads it back, and
// it is what goes in the number column of a database.
func (m Money) Number() string {
	return m.amount.Format(m.currency.Decimals())
}

// String returns the amount as Number writes it, a space and the currency
// code, such as "12.50 USD".
func (m Money) String() string {
	b := make([]byte, 0, len("-9223372036854775807.999999999999999999 ")+len(m.currency.Code()))
	b = m.amount.AppendFormat(b, m.currency.Decimals())
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

// allocationStep returns the unit that Allocate hands out: the smallest unit
// of the currency when the amount is a whole number of them, and the
// smallest number there is when it is finer.
func (m Money) allocationStep() fixed.Decimal {
	// None of these calls can fail: a currency has no more decimal places
	// than a Decimal keeps, rounding toward zero stays in range, and one
	// atto is a number.
	step, _ := fixed.NewDecimalFromInt64(1, m.currency.Decimals())
	if whole, _ := m.amount.RoundToMultiple(step, fixed.RoundDown); whole != m.amount {
		step, _ = fixed.NewDecimal(0, 1)
	}

	return step
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
