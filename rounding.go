package money

import (
	"fmt"
	"math/big"
)

// A RoundingMode says which of the two nearest representable amounts an
// operation picks when its exact result lies between them.
//
// The zero value is not a rounding mode; operations reject it.
type RoundingMode uint8

const (
	// RoundHalfEven rounds to the nearest amount, and an exact half to the
	// one whose last digit is even: 2.5 to 2, 3.5 to 4. It adds no bias over
	// many roundings, and is known as banker's rounding.
	RoundHalfEven RoundingMode = iota + 1

	// RoundHalfUp rounds to the nearest amount, and an exact half away from
	// zero: 2.5 to 3, -2.5 to -3.
	RoundHalfUp

	// RoundHalfDown rounds to the nearest amount, and an exact half toward
	// zero: 2.5 to 2, -2.5 to -2.
	RoundHalfDown

	// RoundUp rounds away from zero: 2.1 to 3, -2.1 to -3.
	RoundUp

	// RoundDown rounds toward zero, dropping the excess: 2.9 to 2, -2.9 to
	// -2.
	RoundDown

	// RoundCeiling rounds toward positive infinity: 2.1 to 3, -2.9 to -2.
	RoundCeiling

	// RoundFloor rounds toward negative infinity: 2.9 to 2, -2.1 to -3.
	RoundFloor
)

// awayFromZero reports whether the mode moves a truncated quotient one step
// away from zero. negative is the sign of the exact quotient, half compares
// the dropped part with half a step (negative when smaller), and odd says
// whether the truncated quotient is odd. The dropped part is never zero.
func (mode RoundingMode) awayFromZero(negative bool, half int, odd bool) (bool, error) {
	switch mode {
	case RoundHalfEven:
		return roundsHalfEvenAway(half, odd), nil
	case RoundHalfUp:
		return half >= 0, nil
	case RoundHalfDown:
		return half > 0, nil
	case RoundUp:
		return true, nil
	case RoundDown:
		return false, nil
	case RoundCeiling:
		return !negative, nil
	case RoundFloor:
		return negative, nil
	default:
		return false, fmt.Errorf("unknown rounding mode %d", uint8(mode))
	}
}

// roundsHalfEvenAway reports whether RoundHalfEven moves a truncated quotient
// away from zero: past the half always, and at the half only from an odd
// quotient, which makes it even.
func roundsHalfEvenAway(half int, odd bool) bool {
	return half > 0 || (half == 0 && odd)
}

// divRound returns numerator divided by denominator, rounded to a whole
// number by the mode. The denominator must not be zero.
func divRound(numerator, denominator *big.Int, mode RoundingMode) (*big.Int, error) {
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))

	// QuoRem truncates toward zero, so the remainder is what was dropped.
	if remainder.Sign() == 0 {
		// Still reject a mode that does not exist, so that a mistake does not
		// depend on the amounts it happens to meet.
		_, err := mode.awayFromZero(false, 0, false)

		return quotient, err
	}

	negative := numerator.Sign() != denominator.Sign()
	half := new(big.Int).Lsh(remainder, 1).CmpAbs(denominator)

	away, err := mode.awayFromZero(negative, half, quotient.Bit(0) == 1)
	if err != nil {
		return nil, err
	}

	if !away {
		return quotient, nil
	}

	if negative {
		return quotient.Sub(quotient, big.NewInt(1)), nil
	}

	return quotient.Add(quotient, big.NewInt(1)), nil
}
