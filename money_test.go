package money_test

import (
	"encoding"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/talav/currency"
	"github.com/talav/fixed"

	"github.com/talav/money"
)

const (
	maxUnits = math.MaxInt64
	maxAttos = 999_999_999_999_999_999
)

// cur returns the currency with the given code: an ISO 4217 currency, or one
// of the custom currencies the tests use.
func cur(t *testing.T, code string) currency.Currency {
	t.Helper()

	custom := map[string]int{"BTC": 8, "ETH": 18}
	if decimals, ok := custom[code]; ok {
		c, err := currency.NewCurrency(code, decimals)
		require.NoError(t, err)

		return c
	}

	c, err := currency.NewCurrencyFromString(code)
	require.NoError(t, err)

	return c
}

// m builds an amount that the test knows to be valid from text such as
// "12.50 USD".
func m(t *testing.T, text string) money.Money {
	t.Helper()

	amount, code, ok := strings.Cut(text, " ")
	require.True(t, ok, "want an amount and a currency code, got %q", text)

	got, err := money.NewMoneyFromString(amount, cur(t, code))
	require.NoError(t, err)

	return got
}

// ms builds one amount for each text.
func ms(t *testing.T, texts ...string) []money.Money {
	t.Helper()

	amounts := make([]money.Money, len(texts))
	for i, text := range texts {
		amounts[i] = m(t, text)
	}

	return amounts
}

func TestNewMoney(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		amount   fixed.Decimal
		currency currency.Currency
		want     string
		wantErr  string
	}{
		{name: "units and a fraction", amount: fixed.MustParse("12.5"), currency: cur(t, "USD"), want: "12.50 USD"},
		{name: "negative fraction alone", amount: fixed.MustParse("-0.25"), currency: cur(t, "USD"), want: "-0.25 USD"},
		{name: "negative units and fraction", amount: fixed.MustParse("-12.5"), currency: cur(t, "USD"), want: "-12.50 USD"},
		{name: "nothing", currency: cur(t, "USD"), want: "0.00 USD"},
		{name: "currency without decimals", amount: fixed.MustParse("500"), currency: cur(t, "JPY"), want: "500 JPY"},
		{name: "one atto", amount: fixed.MustParse("0.000000000000000001"), currency: cur(t, "BTC"), want: "0.000000000000000001 BTC"},
		{name: "currency with eighteen decimals", amount: fixed.MustParse("1.5"), currency: cur(t, "ETH"), want: "1.500000000000000000 ETH"},
		{name: "largest amount", amount: fixed.MustParse("9223372036854775807.999999999999999999"), currency: cur(t, "USD"), want: "9223372036854775807.999999999999999999 USD"},
		{name: "smallest amount", amount: fixed.MustParse("-9223372036854775807.999999999999999999"), currency: cur(t, "USD"), want: "-9223372036854775807.999999999999999999 USD"},
		{name: "zero currency", amount: fixed.MustParse("1"), wantErr: "currency is missing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.NewMoney(tt.amount, tt.currency)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.amount, got.Amount())
			assert.Equal(t, tt.currency, got.Currency())
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestNewMoneyFromMinor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		minor    int64
		currency currency.Currency
		want     string
		wantErr  string
	}{
		{name: "cents", minor: 1250, currency: cur(t, "USD"), want: "12.50 USD"},
		{name: "negative cents", minor: -5, currency: cur(t, "USD"), want: "-0.05 USD"},
		{name: "no minor units", minor: 0, currency: cur(t, "USD"), want: "0.00 USD"},
		{name: "currency without decimals", minor: 1250, currency: cur(t, "JPY"), want: "1250 JPY"},
		{name: "three decimals", minor: 1, currency: cur(t, "BHD"), want: "0.001 BHD"},
		{name: "eight decimals", minor: 150_000_000, currency: cur(t, "BTC"), want: "1.50000000 BTC"},
		{name: "eighteen decimals", minor: 1, currency: cur(t, "ETH"), want: "0.000000000000000001 ETH"},
		{name: "most minor units", minor: math.MaxInt64, currency: cur(t, "USD"), want: "92233720368547758.07 USD"},
		{name: "zero currency", minor: 1, wantErr: "currency is missing"},
		{name: "out of range", minor: math.MinInt64, currency: cur(t, "JPY"), wantErr: "-9223372036854775808 minor units of JPY: units -9223372036854775808: number out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.NewMoneyFromMinor(tt.minor, tt.currency)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())

			minor, err := got.Minor()
			require.NoError(t, err)
			assert.Equal(t, tt.minor, minor)
		})
	}
}

func TestNewMoneyFromString(t *testing.T) {
	t.Parallel()

	const formatErr = "must be a plain decimal number with at most 18 decimal places, such as -12.50"

	tests := []struct {
		name      string
		text      string
		wantUnits int64
		wantAttos int64
		wantErr   string
	}{
		{name: "two decimals", text: "12.50", wantUnits: 12, wantAttos: 500_000_000_000_000_000},
		{name: "whole number", text: "100", wantUnits: 100},
		{name: "finer than the currency", text: "-0.0035", wantAttos: -3_500_000_000_000_000},
		{name: "zero", text: "0"},
		{name: "negative zero", text: "-0.00"},
		{name: "leading zeros", text: "007.5", wantUnits: 7, wantAttos: 500_000_000_000_000_000},
		{name: "surrounding white space", text: " 12.5\n", wantUnits: 12, wantAttos: 500_000_000_000_000_000},
		{name: "eighteen decimals", text: "0.000000000000000001", wantAttos: 1},
		{name: "largest amount", text: "9223372036854775807.999999999999999999", wantUnits: maxUnits, wantAttos: maxAttos},
		{name: "smallest amount", text: "-9223372036854775807.999999999999999999", wantUnits: -maxUnits, wantAttos: -maxAttos},
		{name: "empty", text: "", wantErr: formatErr},
		{name: "word", text: "free", wantErr: formatErr},
		{name: "sign alone", text: "-", wantErr: formatErr},
		{name: "point without decimals", text: "1.", wantErr: formatErr},
		{name: "point without units", text: ".5", wantErr: formatErr},
		{name: "two points", text: "1.2.3", wantErr: formatErr},
		{name: "comma as the point", text: "1,5", wantErr: formatErr},
		{name: "thousands separator", text: "1,000.00", wantErr: formatErr},
		{name: "plus sign", text: "+1", wantErr: formatErr},
		{name: "two signs", text: "--1", wantErr: formatErr},
		{name: "sign after the number", text: "1-", wantErr: formatErr},
		{name: "exponent", text: "1e3", wantErr: formatErr},
		{name: "nineteen decimals", text: "1.1234567890123456789", wantErr: formatErr},
		{name: "space inside", text: "12 50", wantErr: formatErr},
		{name: "currency symbol", text: "$12.50", wantErr: formatErr},
		{name: "non-ASCII digits", text: "１２", wantErr: formatErr},
		{name: "one unit too many", text: "9223372036854775808", wantErr: "number out of range"},
		{name: "one unit too few", text: "-9223372036854775808", wantErr: "number out of range"},
		{name: "far too many digits", text: strings.Repeat("9", 40), wantErr: "number out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.NewMoneyFromString(tt.text, cur(t, "USD"))
			if tt.wantErr != "" {
				require.EqualError(t, err, `invalid decimal "`+tt.text+`": `+tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUnits, got.Amount().Units())
			assert.Equal(t, tt.wantAttos, got.Amount().Attos())
			assert.Equal(t, cur(t, "USD"), got.Currency())
		})
	}
}

func TestNewMoneyFromStringCurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		currency currency.Currency
		wantErr  string
	}{
		{name: "zero currency", wantErr: "currency is missing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.NewMoneyFromString("1.00", tt.currency)

			require.EqualError(t, err, tt.wantErr)
			assert.Equal(t, money.Money{}, got)
		})
	}
}

func TestMoneyMinor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		want    int64
		wantErr string
	}{
		{name: "cents", money: m(t, "12.50 USD"), want: 1250},
		{name: "negative cents", money: m(t, "-0.05 USD"), want: -5},
		{name: "currency without decimals", money: m(t, "1250 JPY"), want: 1250},
		{name: "attos of an eighteen-decimal currency", money: m(t, "1.5 ETH"), want: 1_500_000_000_000_000_000},
		{name: "half a cent", money: m(t, "0.005 USD"), wantErr: "minor units of 0.005 USD: 0.005 has more than 2 decimal places"},
		{name: "fraction of a currency without decimals", money: m(t, "0.5 JPY"), wantErr: "minor units of 0.5 JPY: 0.5 has more than 0 decimal places"},
		{name: "too many minor units", money: m(t, "10 ETH"), wantErr: "minor units of 10.000000000000000000 ETH: 10 with 18 decimal places: number out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Minor()
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Zero(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		money money.Money
		want  string
	}{
		{name: "decimals of the currency", money: m(t, "12.5 USD"), want: "12.50 USD"},
		{name: "whole amount", money: m(t, "7 USD"), want: "7.00 USD"},
		{name: "negative fraction", money: m(t, "-0.05 USD"), want: "-0.05 USD"},
		{name: "finer than the currency", money: m(t, "0.0035 USD"), want: "0.0035 USD"},
		{name: "all eighteen decimals", money: m(t, "1.000000000000000001 USD"), want: "1.000000000000000001 USD"},
		{name: "currency without decimals", money: m(t, "500 JPY"), want: "500 JPY"},
		{name: "fraction of a currency without decimals", money: m(t, "0.5 JPY"), want: "0.5 JPY"},
		{name: "three decimals", money: m(t, "0.001 BHD"), want: "0.001 BHD"},
		{name: "eighteen-decimal currency", money: m(t, "2 ETH"), want: "2.000000000000000000 ETH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.money.String())

			number, code, _ := strings.Cut(tt.want, " ")
			assert.Equal(t, number, tt.money.Number())
			assert.Equal(t, code, tt.money.Currency().Code())

			again, err := money.NewMoneyFromString(tt.money.Number(), tt.money.Currency())
			require.NoError(t, err)
			assert.Equal(t, tt.money, again, "Number must read back as the same amount")
		})
	}
}

func TestMoneySign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		money money.Money
		want  int
	}{
		{name: "positive", money: m(t, "12.50 USD"), want: 1},
		{name: "positive fraction", money: m(t, "0.000000000000000001 USD"), want: 1},
		{name: "nothing", money: m(t, "0 USD"), want: 0},
		{name: "negative fraction", money: m(t, "-0.000000000000000001 USD"), want: -1},
		{name: "negative", money: m(t, "-12.50 USD"), want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.money.Sign())
			assert.Equal(t, tt.want == 0, tt.money.IsZero())
			assert.Equal(t, tt.want > 0, tt.money.IsPositive())
			assert.Equal(t, tt.want < 0, tt.money.IsNegative())
			assert.Equal(t, -tt.want, tt.money.Neg().Sign())
			assert.Equal(t, tt.want*tt.want, tt.money.Abs().Sign())
			assert.Equal(t, tt.money, tt.money.Neg().Neg())
		})
	}
}

func TestMoneyIsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		money money.Money
		want  bool
	}{
		{name: "zero value", money: money.Money{}, want: false},
		{name: "nothing of a currency", money: m(t, "0 USD"), want: true},
		{name: "positive amount", money: m(t, "12.50 USD"), want: true},
		{name: "negative amount", money: m(t, "-0.05 USD"), want: true},
		{name: "custom currency", money: m(t, "1.5 BTC"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.money.IsValid())
		})
	}
}

func TestMoneyEquality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  money.Money
		right money.Money
		want  bool
	}{
		{name: "same amount written two ways", left: m(t, "12.5 USD"), right: m(t, "12.50 USD"), want: true},
		{name: "zero and negative zero", left: m(t, "0 USD"), right: m(t, "-0.00 USD"), want: true},
		{name: "different amounts", left: m(t, "12.50 USD"), right: m(t, "12.51 USD"), want: false},
		{name: "same number, different currencies", left: m(t, "12.50 USD"), right: m(t, "12.50 EUR"), want: false},
		{name: "nothing against the zero value", left: m(t, "0 USD"), right: money.Money{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.left == tt.right)
			assert.Equal(t, tt.left.Currency() == tt.right.Currency(), tt.left.SameCurrency(tt.right))
		})
	}
}

func TestMoneyCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		other   money.Money
		want    int
		wantErr error
	}{
		{name: "equal", money: m(t, "12.50 USD"), other: m(t, "12.50 USD"), want: 0},
		{name: "fewer units", money: m(t, "11.99 USD"), other: m(t, "12.00 USD"), want: -1},
		{name: "more units", money: m(t, "13.00 USD"), other: m(t, "12.99 USD"), want: 1},
		{name: "same units, smaller fraction", money: m(t, "12.49 USD"), other: m(t, "12.50 USD"), want: -1},
		{name: "one atto more", money: m(t, "0.000000000000000001 USD"), other: m(t, "0 USD"), want: 1},
		{name: "negative against positive", money: m(t, "-0.01 USD"), other: m(t, "0.01 USD"), want: -1},
		{name: "two negatives", money: m(t, "-2.50 USD"), other: m(t, "-2.25 USD"), want: -1},
		{name: "two currencies", money: m(t, "1 USD"), other: m(t, "1 EUR"), wantErr: money.ErrCurrencyMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Compare(tt.other)
			lesser, minErr := tt.money.Min(tt.other)
			greater, maxErr := tt.money.Max(tt.other)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorIs(t, minErr, tt.wantErr)
				require.ErrorIs(t, maxErr, tt.wantErr)
				require.EqualError(t, err, "currencies differ: USD and EUR")
				assert.Equal(t, money.Money{}, lesser)
				assert.Equal(t, money.Money{}, greater)

				return
			}

			require.NoError(t, err)
			require.NoError(t, minErr)
			require.NoError(t, maxErr)
			assert.Equal(t, tt.want, got)

			want := []money.Money{tt.money, tt.other}
			if tt.want > 0 {
				want = []money.Money{tt.other, tt.money}
			}

			assert.Equal(t, want, []money.Money{lesser, greater})
		})
	}
}

func TestMoneyAdd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		other   money.Money
		want    money.Money
		wantErr error
	}{
		{name: "whole amounts", money: m(t, "2 USD"), other: m(t, "3 USD"), want: m(t, "5 USD")},
		{name: "fractions that carry", money: m(t, "0.60 USD"), other: m(t, "0.60 USD"), want: m(t, "1.20 USD")},
		{name: "negative fractions that carry", money: m(t, "-0.60 USD"), other: m(t, "-0.60 USD"), want: m(t, "-1.20 USD")},
		{name: "smaller debit borrows from the units", money: m(t, "1 USD"), other: m(t, "-0.25 USD"), want: m(t, "0.75 USD")},
		{name: "larger debit turns the sum negative", money: m(t, "0.25 USD"), other: m(t, "-1 USD"), want: m(t, "-0.75 USD")},
		{name: "opposite amounts", money: m(t, "12.50 USD"), other: m(t, "-12.50 USD"), want: m(t, "0 USD")},
		{name: "nothing added", money: m(t, "12.50 USD"), other: m(t, "0 USD"), want: m(t, "12.50 USD")},
		{name: "sub-cent amounts", money: m(t, "0.0035 USD"), other: m(t, "0.0065 USD"), want: m(t, "0.01 USD")},
		{name: "up to the largest amount", money: m(t, "9223372036854775807 USD"), other: m(t, "0.999999999999999999 USD"), want: m(t, "9223372036854775807.999999999999999999 USD")},
		{name: "down from the largest amount", money: m(t, "9223372036854775807 USD"), other: m(t, "-0.5 USD"), want: m(t, "9223372036854775806.5 USD")},
		{name: "units past the largest", money: m(t, "9223372036854775807 USD"), other: m(t, "1 USD"), wantErr: fixed.ErrOverflow},
		{name: "carry past the largest", money: m(t, "9223372036854775807.6 USD"), other: m(t, "0.6 USD"), wantErr: fixed.ErrOverflow},
		{name: "units past the smallest", money: m(t, "-9223372036854775807 USD"), other: m(t, "-1 USD"), wantErr: fixed.ErrOverflow},
		{name: "carry past the smallest", money: m(t, "-9223372036854775807.6 USD"), other: m(t, "-0.6 USD"), wantErr: fixed.ErrOverflow},
		{name: "two currencies", money: m(t, "1 USD"), other: m(t, "1 EUR"), wantErr: money.ErrCurrencyMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Add(tt.other)
			swapped, swappedErr := tt.other.Add(tt.money)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorIs(t, swappedErr, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			require.NoError(t, swappedErr)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want, swapped)

			back, err := got.Sub(tt.other)
			require.NoError(t, err)
			assert.Equal(t, tt.money, back, "subtracting what was added must give the first amount back")
		})
	}
}

func TestMoneySub(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		other   money.Money
		want    money.Money
		wantErr error
	}{
		{name: "smaller from larger", money: m(t, "5 USD"), other: m(t, "3.25 USD"), want: m(t, "1.75 USD")},
		{name: "larger from smaller", money: m(t, "3.25 USD"), other: m(t, "5 USD"), want: m(t, "-1.75 USD")},
		{name: "debit from credit", money: m(t, "1 USD"), other: m(t, "-1 USD"), want: m(t, "2 USD")},
		{name: "itself", money: m(t, "12.50 USD"), other: m(t, "12.50 USD"), want: m(t, "0 USD")},
		{name: "past the largest", money: m(t, "9223372036854775807 USD"), other: m(t, "-1 USD"), wantErr: fixed.ErrOverflow},
		{name: "two currencies", money: m(t, "1 USD"), other: m(t, "1 JPY"), wantErr: money.ErrCurrencyMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Sub(tt.other)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyMul(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		n       int64
		want    money.Money
		wantErr string
	}{
		{name: "several items", money: m(t, "12.50 USD"), n: 3, want: m(t, "37.50 USD")},
		{name: "one item", money: m(t, "12.50 USD"), n: 1, want: m(t, "12.50 USD")},
		{name: "no items", money: m(t, "12.50 USD"), n: 0, want: m(t, "0 USD")},
		{name: "negative count", money: m(t, "12.50 USD"), n: -2, want: m(t, "-25 USD")},
		{name: "sub-cent price", money: m(t, "0.0035 USD"), n: 100, want: m(t, "0.35 USD")},
		{name: "fraction that carries into the units", money: m(t, "0.999999999 USD"), n: 1_000_000_000, want: m(t, "999999999 USD")},
		{name: "largest count of an atto", money: m(t, "0.000000000000000001 USD"), n: math.MaxInt64, want: m(t, "9.223372036854775807 USD")},
		{name: "out of range", money: m(t, "9223372036854775807 USD"), n: 2, wantErr: "9223372036854775807 times 2: number out of range"},
		{name: "out of range below", money: m(t, "5000000000 USD"), n: math.MinInt64, wantErr: "5000000000 times -9223372036854775808: number out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Mul(tt.n)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				require.ErrorIs(t, err, fixed.ErrOverflow)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyMulRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		money       money.Money
		numerator   int64
		denominator int64
		mode        fixed.RoundingMode
		want        money.Money
		wantErr     string
	}{
		{name: "percentage", money: m(t, "100 USD"), numerator: 825, denominator: 10000, mode: fixed.RoundHalfEven, want: m(t, "8.25 USD")},
		{name: "a third, rounded down at the eighteenth decimal", money: m(t, "10 USD"), numerator: 1, denominator: 3, mode: fixed.RoundHalfEven, want: m(t, "3.333333333333333333 USD")},
		{name: "two thirds, rounded up at the eighteenth decimal", money: m(t, "10 USD"), numerator: 2, denominator: 3, mode: fixed.RoundHalfEven, want: m(t, "6.666666666666666667 USD")},
		{name: "two thirds, truncated", money: m(t, "10 USD"), numerator: 2, denominator: 3, mode: fixed.RoundDown, want: m(t, "6.666666666666666666 USD")},
		{name: "negative numerator", money: m(t, "10 USD"), numerator: -1, denominator: 4, mode: fixed.RoundHalfEven, want: m(t, "-2.50 USD")},
		{name: "negative denominator", money: m(t, "10 USD"), numerator: 1, denominator: -4, mode: fixed.RoundHalfEven, want: m(t, "-2.50 USD")},
		{name: "both negative", money: m(t, "10 USD"), numerator: -1, denominator: -4, mode: fixed.RoundHalfEven, want: m(t, "2.50 USD")},
		{name: "zero numerator", money: m(t, "10 USD"), numerator: 0, denominator: 7, mode: fixed.RoundHalfEven, want: m(t, "0 USD")},
		{name: "largest amount by one", money: m(t, "9223372036854775807.999999999999999999 USD"), numerator: math.MaxInt64, denominator: math.MaxInt64, mode: fixed.RoundHalfEven, want: m(t, "9223372036854775807.999999999999999999 USD")},
		{name: "zero denominator", money: m(t, "10 USD"), numerator: 1, denominator: 0, mode: fixed.RoundHalfEven, wantErr: "10 times 1/0: division by zero"},
		{name: "no rounding mode, exact result", money: m(t, "10 USD"), numerator: 1, denominator: 2, wantErr: "10 times 1/2: unknown rounding mode 0"},
		{name: "no rounding mode, inexact result", money: m(t, "10 USD"), numerator: 1, denominator: 3, wantErr: "10 times 1/3: unknown rounding mode 0"},
		{name: "rounding mode that does not exist", money: m(t, "10 USD"), numerator: 1, denominator: 3, mode: fixed.RoundingMode(99), wantErr: "10 times 1/3: unknown rounding mode 99"},
		{name: "out of range", money: m(t, "9223372036854775807 USD"), numerator: 3, denominator: 2, mode: fixed.RoundHalfEven, wantErr: "9223372036854775807 times 3/2: number out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.MulRatio(tt.numerator, tt.denominator, tt.mode)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyDiv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		n       int64
		mode    fixed.RoundingMode
		want    money.Money
		wantErr string
	}{
		{name: "evenly", money: m(t, "10 USD"), n: 4, mode: fixed.RoundHalfEven, want: m(t, "2.50 USD")},
		{name: "by one", money: m(t, "12.50 USD"), n: 1, mode: fixed.RoundHalfEven, want: m(t, "12.50 USD")},
		{name: "unevenly", money: m(t, "10 USD"), n: 3, mode: fixed.RoundHalfEven, want: m(t, "3.333333333333333333 USD")},
		{name: "unevenly, rounded up", money: m(t, "10 USD"), n: 3, mode: fixed.RoundUp, want: m(t, "3.333333333333333334 USD")},
		{name: "by a negative number", money: m(t, "10 USD"), n: -4, mode: fixed.RoundHalfEven, want: m(t, "-2.50 USD")},
		{name: "one atto in two", money: m(t, "0.000000000000000001 USD"), n: 2, mode: fixed.RoundHalfEven, want: m(t, "0 USD")},
		{name: "one atto in two, half up", money: m(t, "0.000000000000000001 USD"), n: 2, mode: fixed.RoundHalfUp, want: m(t, "0.000000000000000001 USD")},
		{name: "by zero", money: m(t, "10 USD"), n: 0, mode: fixed.RoundHalfEven, wantErr: "division by zero"},
		{name: "no rounding mode", money: m(t, "10 USD"), n: 3, wantErr: "10 times 1/3: unknown rounding mode 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Div(tt.n, tt.mode)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyRound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		mode    fixed.RoundingMode
		want    money.Money
		wantErr string
	}{
		{name: "already whole cents", money: m(t, "12.50 USD"), mode: fixed.RoundHalfEven, want: m(t, "12.50 USD")},
		{name: "half a cent to the even cent below", money: m(t, "0.005 USD"), mode: fixed.RoundHalfEven, want: m(t, "0 USD")},
		{name: "half a cent to the even cent above", money: m(t, "0.015 USD"), mode: fixed.RoundHalfEven, want: m(t, "0.02 USD")},
		{name: "half a cent, half up", money: m(t, "0.005 USD"), mode: fixed.RoundHalfUp, want: m(t, "0.01 USD")},
		{name: "sub-cent fee, rounded up", money: m(t, "0.0035 USD"), mode: fixed.RoundUp, want: m(t, "0.01 USD")},
		{name: "negative amount", money: m(t, "-1.005 USD"), mode: fixed.RoundHalfUp, want: m(t, "-1.01 USD")},
		{name: "currency without decimals", money: m(t, "0.5 JPY"), mode: fixed.RoundHalfUp, want: m(t, "1 JPY")},
		{name: "three decimals", money: m(t, "1.23456 BHD"), mode: fixed.RoundHalfEven, want: m(t, "1.235 BHD")},
		{name: "eighteen decimals leave nothing to round", money: m(t, "1.000000000000000001 ETH"), mode: fixed.RoundDown, want: m(t, "1.000000000000000001 ETH")},
		{name: "rounding carries into the units", money: m(t, "1.999 USD"), mode: fixed.RoundHalfEven, want: m(t, "2 USD")},
		{name: "no rounding mode", money: m(t, "0.005 USD"), wantErr: "round 0.005 to 2 decimal places: unknown rounding mode 0"},
		{name: "no rounding mode, nothing to round", money: m(t, "12.50 USD"), wantErr: "round 12.5 to 2 decimal places: unknown rounding mode 0"},
		{name: "rounded past the largest amount", money: m(t, "9223372036854775807.999 USD"), mode: fixed.RoundUp, wantErr: "round 9223372036854775807.999 to 2 decimal places: number out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Round(tt.mode)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyRoundTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		money    money.Money
		decimals int
		want     money.Money
		wantErr  string
	}{
		{name: "to whole units", money: m(t, "12.50 USD"), decimals: 0, want: m(t, "12 USD")},
		{name: "to four decimals", money: m(t, "1.23456789 USD"), decimals: 4, want: m(t, "1.2346 USD")},
		{name: "to eighteen decimals", money: m(t, "1.234567891234567891 USD"), decimals: 18, want: m(t, "1.234567891234567891 USD")},
		{name: "finer than the amount", money: m(t, "1.5 USD"), decimals: 6, want: m(t, "1.5 USD")},
		{name: "negative decimals", money: m(t, "1.5 USD"), decimals: -1, wantErr: "decimals -1 must be in 0..18"},
		{name: "nineteen decimals", money: m(t, "1.5 USD"), decimals: 19, wantErr: "decimals 19 must be in 0..18"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.RoundTo(tt.decimals, fixed.RoundHalfEven)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyRoundToMultiple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		step    money.Money
		mode    fixed.RoundingMode
		want    money.Money
		wantErr string
	}{
		{name: "down to the five cents below", money: m(t, "1.02 USD"), step: m(t, "0.05 USD"), mode: fixed.RoundHalfUp, want: m(t, "1.00 USD")},
		{name: "up to the five cents above", money: m(t, "1.03 USD"), step: m(t, "0.05 USD"), mode: fixed.RoundHalfUp, want: m(t, "1.05 USD")},
		{name: "halfway, to the even multiple", money: m(t, "1.025 USD"), step: m(t, "0.05 USD"), mode: fixed.RoundHalfEven, want: m(t, "1.00 USD")},
		{name: "halfway, half up", money: m(t, "1.025 USD"), step: m(t, "0.05 USD"), mode: fixed.RoundHalfUp, want: m(t, "1.05 USD")},
		{name: "already a multiple", money: m(t, "1.25 USD"), step: m(t, "0.25 USD"), mode: fixed.RoundHalfEven, want: m(t, "1.25 USD")},
		{name: "negative amount toward the floor", money: m(t, "-1.02 USD"), step: m(t, "0.05 USD"), mode: fixed.RoundFloor, want: m(t, "-1.05 USD")},
		{name: "step larger than a unit", money: m(t, "130 JPY"), step: m(t, "50 JPY"), mode: fixed.RoundHalfEven, want: m(t, "150 JPY")},
		{name: "step of zero", money: m(t, "1.02 USD"), step: m(t, "0 USD"), mode: fixed.RoundHalfEven, wantErr: "step 0 must be positive"},
		{name: "negative step", money: m(t, "1.02 USD"), step: m(t, "-0.05 USD"), mode: fixed.RoundHalfEven, wantErr: "step -0.05 must be positive"},
		{name: "step in another currency", money: m(t, "1.02 USD"), step: m(t, "0.05 EUR"), mode: fixed.RoundHalfEven, wantErr: "currencies differ: USD and EUR"},
		{name: "no rounding mode", money: m(t, "1.02 USD"), step: m(t, "0.05 USD"), wantErr: "round 1.02 to a multiple of 0.05: unknown rounding mode 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.RoundToMultiple(tt.step, tt.mode)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRoundingModes(t *testing.T) {
	t.Parallel()

	// Each mode rounds these amounts to whole units.
	inputs := []string{"2.5 JPY", "3.5 JPY", "-2.5 JPY", "-3.5 JPY", "2.4 JPY", "2.6 JPY", "-2.4 JPY", "-2.6 JPY", "2 JPY"}

	tests := []struct {
		name string
		mode fixed.RoundingMode
		want []string
	}{
		{name: "half even", mode: fixed.RoundHalfEven, want: []string{"2 JPY", "4 JPY", "-2 JPY", "-4 JPY", "2 JPY", "3 JPY", "-2 JPY", "-3 JPY", "2 JPY"}},
		{name: "half up", mode: fixed.RoundHalfUp, want: []string{"3 JPY", "4 JPY", "-3 JPY", "-4 JPY", "2 JPY", "3 JPY", "-2 JPY", "-3 JPY", "2 JPY"}},
		{name: "half down", mode: fixed.RoundHalfDown, want: []string{"2 JPY", "3 JPY", "-2 JPY", "-3 JPY", "2 JPY", "3 JPY", "-2 JPY", "-3 JPY", "2 JPY"}},
		{name: "up", mode: fixed.RoundUp, want: []string{"3 JPY", "4 JPY", "-3 JPY", "-4 JPY", "3 JPY", "3 JPY", "-3 JPY", "-3 JPY", "2 JPY"}},
		{name: "down", mode: fixed.RoundDown, want: []string{"2 JPY", "3 JPY", "-2 JPY", "-3 JPY", "2 JPY", "2 JPY", "-2 JPY", "-2 JPY", "2 JPY"}},
		{name: "ceiling", mode: fixed.RoundCeiling, want: []string{"3 JPY", "4 JPY", "-2 JPY", "-3 JPY", "3 JPY", "3 JPY", "-2 JPY", "-2 JPY", "2 JPY"}},
		{name: "floor", mode: fixed.RoundFloor, want: []string{"2 JPY", "3 JPY", "-3 JPY", "-4 JPY", "2 JPY", "2 JPY", "-3 JPY", "-3 JPY", "2 JPY"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := make([]string, len(inputs))

			for i, input := range inputs {
				rounded, err := m(t, input).Round(tt.mode)
				require.NoError(t, err)

				got[i] = rounded.String()
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyAllocate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		ratios  []int64
		want    []money.Money
		wantErr string
	}{
		{name: "three equal shares, one cent over", money: m(t, "100 USD"), ratios: []int64{1, 1, 1}, want: ms(t, "33.34 USD", "33.33 USD", "33.33 USD")},
		{name: "three equal shares, two cents over", money: m(t, "0.05 USD"), ratios: []int64{1, 1, 1}, want: ms(t, "0.02 USD", "0.02 USD", "0.01 USD")},
		{name: "unequal shares", money: m(t, "0.05 USD"), ratios: []int64{3, 7}, want: ms(t, "0.02 USD", "0.03 USD")},
		{name: "shares that divide evenly", money: m(t, "100 USD"), ratios: []int64{70, 20, 10}, want: ms(t, "70 USD", "20 USD", "10 USD")},
		{name: "one party", money: m(t, "12.50 USD"), ratios: []int64{5}, want: ms(t, "12.50 USD")},
		{name: "debt shared like a credit", money: m(t, "-100 USD"), ratios: []int64{1, 1, 1}, want: ms(t, "-33.34 USD", "-33.33 USD", "-33.33 USD")},
		{name: "party with no ratio gets no leftover", money: m(t, "0.01 USD"), ratios: []int64{0, 1, 1}, want: ms(t, "0 USD", "0.01 USD", "0 USD")},
		{name: "nothing to share", money: m(t, "0 USD"), ratios: []int64{1, 2}, want: ms(t, "0 USD", "0 USD")},
		{name: "currency without decimals", money: m(t, "100 JPY"), ratios: []int64{1, 2}, want: ms(t, "34 JPY", "66 JPY")},
		{name: "amount finer than a cent is shared to the atto", money: m(t, "0.0035 USD"), ratios: []int64{1, 1}, want: ms(t, "0.00175 USD", "0.00175 USD")},
		{name: "one atto", money: m(t, "0.000000000000000001 USD"), ratios: []int64{1, 1, 1}, want: ms(t, "0.000000000000000001 USD", "0 USD", "0 USD")},
		{name: "largest ratios", money: m(t, "9223372036854775807.99 USD"), ratios: []int64{math.MaxInt64, math.MaxInt64}, want: ms(t, "4611686018427387904.00 USD", "4611686018427387903.99 USD")},
		{name: "no ratios", money: m(t, "100 USD"), wantErr: "no ratios to allocate by"},
		{name: "negative ratio", money: m(t, "100 USD"), ratios: []int64{1, -1}, wantErr: "ratio -1 must not be negative"},
		{name: "all ratios zero", money: m(t, "100 USD"), ratios: []int64{0, 0}, wantErr: "ratios must not all be zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Allocate(tt.ratios...)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			sum := m(t, "0 "+tt.money.Currency().Code())
			for _, share := range got {
				sum, err = sum.Add(share)
				require.NoError(t, err)
			}

			assert.Equal(t, tt.money, sum, "the shares must add up to the amount")
		})
	}
}

func TestMoneySplit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		parts   int
		want    []money.Money
		wantErr string
	}{
		{name: "three parts, one cent over", money: m(t, "10 USD"), parts: 3, want: ms(t, "3.34 USD", "3.33 USD", "3.33 USD")},
		{name: "evenly", money: m(t, "10 USD"), parts: 4, want: ms(t, "2.50 USD", "2.50 USD", "2.50 USD", "2.50 USD")},
		{name: "one part", money: m(t, "10 USD"), parts: 1, want: ms(t, "10 USD")},
		{name: "more parts than cents", money: m(t, "0.02 USD"), parts: 3, want: ms(t, "0.01 USD", "0.01 USD", "0 USD")},
		{name: "no parts", money: m(t, "10 USD"), parts: 0, wantErr: "parts 0 must be at least 1"},
		{name: "negative parts", money: m(t, "10 USD"), parts: -2, wantErr: "parts -2 must be at least 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Split(tt.parts)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyConvert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		money       money.Money
		to          currency.Currency
		numerator   int64
		denominator int64
		mode        fixed.RoundingMode
		want        money.Money
		wantErr     string
	}{
		{name: "rate below one", money: m(t, "100 USD"), to: cur(t, "EUR"), numerator: 9214, denominator: 10000, mode: fixed.RoundHalfEven, want: m(t, "92.14 EUR")},
		{name: "rate above one", money: m(t, "100 USD"), to: cur(t, "JPY"), numerator: 15025, denominator: 100, mode: fixed.RoundHalfEven, want: m(t, "15025 JPY")},
		{name: "result finer than the new currency", money: m(t, "0.01 USD"), to: cur(t, "JPY"), numerator: 15025, denominator: 100, mode: fixed.RoundHalfEven, want: m(t, "1.5025 JPY")},
		{name: "debt", money: m(t, "-100 USD"), to: cur(t, "EUR"), numerator: 9214, denominator: 10000, mode: fixed.RoundHalfEven, want: m(t, "-92.14 EUR")},
		{name: "inexact rate", money: m(t, "1 USD"), to: cur(t, "EUR"), numerator: 1, denominator: 3, mode: fixed.RoundHalfEven, want: m(t, "0.333333333333333333 EUR")},
		{name: "same currency", money: m(t, "100 USD"), to: cur(t, "USD"), numerator: 1, denominator: 1, mode: fixed.RoundHalfEven, want: m(t, "100 USD")},
		{name: "currency with eighteen decimals", money: m(t, "100 USD"), to: cur(t, "ETH"), numerator: 1, denominator: 1, mode: fixed.RoundHalfEven, want: m(t, "100 ETH")},
		{name: "zero currency", money: m(t, "100 USD"), numerator: 1, denominator: 1, mode: fixed.RoundHalfEven, wantErr: "currency is missing"},
		{name: "zero rate", money: m(t, "100 USD"), to: cur(t, "EUR"), numerator: 0, denominator: 1, mode: fixed.RoundHalfEven, wantErr: "rate 0/1 must be positive"},
		{name: "negative rate", money: m(t, "100 USD"), to: cur(t, "EUR"), numerator: -1, denominator: 2, mode: fixed.RoundHalfEven, wantErr: "rate -1/2 must be positive"},
		{name: "zero denominator", money: m(t, "100 USD"), to: cur(t, "EUR"), numerator: 1, denominator: 0, mode: fixed.RoundHalfEven, wantErr: "rate 1/0 must be positive"},
		{name: "no rounding mode", money: m(t, "100 USD"), to: cur(t, "EUR"), numerator: 1, denominator: 3, wantErr: "100 times 1/3: unknown rounding mode 0"},
		{name: "out of range", money: m(t, "9223372036854775807 USD"), to: cur(t, "JPY"), numerator: 150, denominator: 1, mode: fixed.RoundHalfEven, wantErr: "9223372036854775807 times 150/1: number out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.money.Convert(tt.to, tt.numerator, tt.denominator, tt.mode)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, money.Money{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyText(t *testing.T) {
	t.Parallel()

	var (
		_ encoding.TextMarshaler   = money.Money{}
		_ encoding.TextUnmarshaler = &money.Money{}
	)

	tests := []struct {
		name  string
		money money.Money
		want  string
	}{
		{name: "two decimals", money: m(t, "12.5 USD"), want: "12.50 USD"},
		{name: "negative", money: m(t, "-0.05 USD"), want: "-0.05 USD"},
		{name: "finer than the currency", money: m(t, "0.0035 USD"), want: "0.0035 USD"},
		{name: "currency without decimals", money: m(t, "500 JPY"), want: "500 JPY"},
		{name: "nothing", money: m(t, "0 EUR"), want: "0.00 EUR"},
		{name: "largest amount", money: m(t, "9223372036854775807.999999999999999999 USD"), want: "9223372036854775807.999999999999999999 USD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text, err := tt.money.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(text))

			var parsed money.Money

			require.NoError(t, parsed.UnmarshalText(text))
			assert.Equal(t, tt.money, parsed)
		})
	}
}

func TestMoneyUnmarshalText(t *testing.T) {
	t.Parallel()

	const formatErr = "must be a number and a currency code, such as 12.50 USD"

	tests := []struct {
		name    string
		text    string
		want    money.Money
		wantErr string
	}{
		{name: "number and code", text: "12.50 USD", want: m(t, "12.50 USD")},
		{name: "code in lower case", text: "12.50 usd", want: m(t, "12.50 USD")},
		{name: "extra white space", text: "  -7\t JPY\n", want: m(t, "-7 JPY")},
		{name: "empty", text: "", wantErr: formatErr},
		{name: "number alone", text: "12.50", wantErr: formatErr},
		{name: "code alone", text: "USD", wantErr: formatErr},
		{name: "three parts", text: "12.50 USD net", wantErr: formatErr},
		{name: "code first", text: "USD 12.50", wantErr: `invalid currency "12.50": not an ISO 4217 code`},
		{name: "no space", text: "12.50USD", wantErr: formatErr},
		{name: "unknown code", text: "12.50 XYZ", wantErr: `invalid currency "XYZ": not an ISO 4217 code`},
		{name: "currency outside ISO 4217", text: "1.5 BTC", wantErr: `invalid currency "BTC": not an ISO 4217 code`},
		{name: "not a number", text: "lots USD", wantErr: `invalid decimal "lots": must be a plain decimal number with at most 18 decimal places, such as -12.50`},
		{name: "out of range", text: "9223372036854775808 USD", wantErr: `invalid decimal "9223372036854775808": number out of range`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before := m(t, "1 EUR")
			got := before

			err := got.UnmarshalText([]byte(tt.text))
			if tt.wantErr != "" {
				require.EqualError(t, err, `invalid money "`+tt.text+`": `+tt.wantErr)
				assert.Equal(t, before, got, "a failed parse must leave the amount unchanged")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoneyJSON(t *testing.T) {
	t.Parallel()

	var (
		_ json.Marshaler   = money.Money{}
		_ json.Unmarshaler = &money.Money{}
	)

	tests := []struct {
		name  string
		money money.Money
		want  string
	}{
		{name: "two decimals", money: m(t, "12.5 USD"), want: `{"amount":"12.50","currency":"USD"}`},
		{name: "negative", money: m(t, "-0.05 USD"), want: `{"amount":"-0.05","currency":"USD"}`},
		{name: "finer than the currency", money: m(t, "0.0035 USD"), want: `{"amount":"0.0035","currency":"USD"}`},
		{name: "currency without decimals", money: m(t, "500 JPY"), want: `{"amount":"500","currency":"JPY"}`},
		{name: "custom currency", money: m(t, "1.5 BTC"), want: `{"amount":"1.50000000","currency":"BTC"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.money)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(data))

			var parsed money.Money

			err = json.Unmarshal(data, &parsed)
			if tt.money.Currency().NumericCode() == 0 {
				require.Error(t, err, "a custom currency cannot be read back")
				assert.Equal(t, money.Money{}, parsed)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.money, parsed)
		})
	}
}

func TestMoneyJSONInStructs(t *testing.T) {
	t.Parallel()

	type payload struct {
		Total    money.Money            `json:"total"`
		Discount *money.Money           `json:"discount"`
		Paid     map[money.Money]string `json:"paid"`
	}

	in := payload{
		Total: m(t, "12.50 USD"),
		Paid:  map[money.Money]string{m(t, "10 USD"): "card"},
	}

	data, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{"total":{"amount":"12.50","currency":"USD"},"discount":null,"paid":{"10.00 USD":"card"}}`, string(data))

	var out payload

	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

func TestMoneyUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		want    money.Money
		wantErr string
	}{
		{name: "object", data: `{"amount":"12.50","currency":"USD"}`, want: m(t, "12.50 USD")},
		{name: "fields in the other order", data: `{"currency":"JPY","amount":"-7"}`, want: m(t, "-7 JPY")},
		{name: "unknown field ignored", data: `{"amount":"1","currency":"EUR","note":"x"}`, want: m(t, "1 EUR")},
		{name: "null leaves the amount as it was", data: `null`, want: m(t, "3 EUR")},
		{name: "no currency", data: `{"amount":"12.50"}`, wantErr: "invalid money: currency is missing"},
		{name: "no amount", data: `{"currency":"USD"}`, wantErr: `invalid money: invalid decimal "": must be a plain decimal number with at most 18 decimal places, such as -12.50`},
		{name: "empty object", data: `{}`, wantErr: "invalid money: currency is missing"},
		{name: "amount as a JSON number", data: `{"amount":12.50,"currency":"USD"}`, wantErr: "invalid money: json: cannot unmarshal number into Go struct field moneyJSON.amount of type string"},
		{name: "currency outside ISO 4217", data: `{"amount":"1.5","currency":"BTC"}`, wantErr: `invalid money: invalid currency "BTC": not an ISO 4217 code`},
		{name: "text form", data: `"12.50 USD"`, wantErr: "invalid money: json: cannot unmarshal string into Go value of type money.moneyJSON"},
		{name: "bare number", data: `12.50`, wantErr: "invalid money: json: cannot unmarshal number into Go value of type money.moneyJSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before := m(t, "3 EUR")
			got := before

			err := json.Unmarshal([]byte(tt.data), &got)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Equal(t, before, got, "a failed decode must leave the amount unchanged")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
