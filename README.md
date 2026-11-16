# money

[![tag](https://img.shields.io/github/tag/talav/money.svg)](https://github.com/talav/money/tags)
[![Go Reference](https://pkg.go.dev/badge/github.com/talav/money.svg)](https://pkg.go.dev/github.com/talav/money)
[![Go Report Card](https://goreportcard.com/badge/github.com/talav/money)](https://goreportcard.com/report/github.com/talav/money)
[![CI](https://github.com/talav/money/workflows/Money%20CI/badge.svg)](https://github.com/talav/money/actions)
[![codecov](https://codecov.io/gh/talav/money/graph/badge.svg)](https://codecov.io/gh/talav/money)
[![License](https://img.shields.io/github/license/talav/money)](./LICENSE)

An amount of money for Go: exact to 9 decimal places, tied to its currency, and impossible to build in an invalid state.

## Features

- **Exact**: integer arithmetic throughout, no binary floating point
- **9 decimal places for every currency**: a per-item price, a fee or an average keeps its fraction until you round
- **Currencies never mix**: adding dollars to euros is an error, not a number
- **Valid by construction**: constructors and operations return an error instead of an invalid amount
- **Overflow is an error**: no silent wrap-around
- **Full arithmetic**: add, subtract, multiply, divide, scale by a ratio, convert
- **Seven rounding modes**, to the currency's decimals, to any number of decimals, or to a step such as 0.05
- **Allocation without loss**: split an amount into shares that always add up to it
- **Comparable**: values work with `==` and as map keys
- **Text and JSON**: `encoding.TextMarshaler` and `json.Marshaler`
- **Two database columns**: a number and a currency code, the layout that sorts, sums and indexes

Formatting for display by locale (symbols, thousands separators) is not part of this package.

## Installation

```bash
go get github.com/talav/money
```

Requires Go 1.27 or later. Currencies come from [`github.com/talav/currency`](https://github.com/talav/currency).

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/talav/currency"
    "github.com/talav/money"
)

func main() {
    usd, err := currency.NewCurrencyFromString("USD")
    if err != nil {
        log.Fatal(err)
    }

    price, err := money.NewMoneyFromString("19.99", usd)
    if err != nil {
        log.Fatal(err)
    }

    total, err := price.Mul(3)
    if err != nil {
        log.Fatal(err)
    }

    tax, err := total.MulRatio(825, 10000, money.RoundHalfEven) // 8.25%
    if err != nil {
        log.Fatal(err)
    }

    tax, err = tax.Round(money.RoundHalfEven)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(total) // 59.97 USD
    fmt.Println(tax)   // 4.95 USD
}
```

## How an amount is stored

An amount is two whole numbers: the units of the currency, and the nanos, which are billionths of a unit. `12.50` is 12 units and 500,000,000 nanos.

- **Precision**: 9 decimal places, the same for every currency.
- **Range**: about ±9.2 quintillion units (the whole part fits in 63 bits).
- **One form per amount**: equal amounts are equal with `==`.

The amount is finer than what most currencies can pay. `0.0035 USD` is a valid amount; `Round` turns it into cents when the time comes.

A currency with more than 9 decimal places cannot be used.

## Usage

### Creating

```go
a, err := money.NewMoneyFromString("12.50", usd)      // decimal text
b, err := money.NewMoneyFromMinor(1250, usd)          // 1250 cents
c, err := money.NewMoney(12, 500_000_000, usd)        // units and nanos
```

`NewMoneyFromString` reads digits, an optional point with one to nine more digits, and an optional leading minus sign. It does not read thousands separators, exponents or currency symbols.

### Reading

```go
a.Currency() // USD
a.Units()    // 12
a.Nanos()    // 500000000
a.Minor()    // 1250, nil; an error if the amount is finer than a cent
a.Number()   // "12.50"
a.String()   // "12.50 USD"

a.IsValid()    // true; false only for money.Money{}
a.Sign()       // 1
a.IsZero()     // false
a.IsPositive() // true
a.IsNegative() // false
```

### Comparing

```go
a == b               // true: same amount, same currency
a.Compare(b)         // 0, nil
a.Min(b), a.Max(b)   // the lesser and the greater
a.SameCurrency(b)    // true
```

`Compare`, `Min` and `Max` return `money.ErrCurrencyMismatch` for two currencies.

### Arithmetic

```go
sum, err := a.Add(b)
diff, err := a.Sub(b)
debit := a.Neg()
size := a.Abs()

three, err := a.Mul(3)                                    // exact
each, err := a.Div(3, money.RoundHalfEven)                // rounded at the 9th decimal
tax, err := a.MulRatio(825, 10000, money.RoundHalfEven)   // times 8.25%
```

A factor that is not a whole number is given as a ratio of two whole numbers, which states it exactly: 8.25% is 825/10000 and a third is 1/3.

Every operation that can leave the range returns `money.ErrOverflow`.

### Rounding

```go
cents, err := a.Round(money.RoundHalfEven)          // to the currency's decimals
four, err := a.RoundTo(4, money.RoundHalfEven)      // to 4 decimals
cash, err := a.RoundToMultiple(nickel, money.RoundHalfUp) // to a step, such as 0.05
```

| Mode | 2.5 | 3.5 | -2.5 | 2.4 | 2.6 |
|------|-----|-----|------|-----|-----|
| `RoundHalfEven` | 2 | 4 | -2 | 2 | 3 |
| `RoundHalfUp` | 3 | 4 | -3 | 2 | 3 |
| `RoundHalfDown` | 2 | 3 | -2 | 2 | 3 |
| `RoundUp` | 3 | 4 | -3 | 3 | 3 |
| `RoundDown` | 2 | 3 | -2 | 2 | 2 |
| `RoundCeiling` | 3 | 4 | -2 | 3 | 3 |
| `RoundFloor` | 2 | 3 | -3 | 2 | 2 |

There is no default mode: the zero `RoundingMode` is rejected, so rounding is always a stated choice.

### Allocating

Dividing 100.00 by 3 and multiplying back loses a cent. `Allocate` and `Split` never do:

```go
shares, err := hundred.Split(3)
// 33.34 USD, 33.33 USD, 33.33 USD

shares, err = hundred.Allocate(70, 20, 10)
// 70.00 USD, 20.00 USD, 10.00 USD
```

What the proportions leave over is handed out one smallest unit at a time, starting with the first share. The smallest unit is the currency's (a cent for USD); an amount finer than that is shared out to the nano.

### Converting

```go
eur, err := hundred.Convert(euro, 9214, 10000, money.RoundHalfEven) // rate 0.9214
// 92.14 EUR
```

The result keeps its 9 decimal places; `Round` brings it to those of the new currency.

### JSON

An amount is a JSON object. The number is a string, so that no reader turns it into a binary fraction:

```go
type Invoice struct {
    Total money.Money `json:"total"`
}

// {"total":{"amount":"12.50","currency":"USD"}}
```

Reading requires the amount to be a string; a JSON number is rejected. A JSON `null` leaves the value untouched; use a `*money.Money` for an optional amount.

As a map key, and as plain text anywhere else, an amount is written `12.50 USD`.

### SQL

An amount takes two columns: a number and a currency code. That keeps the number a number, so the database can sort, sum and index it.

```sql
CREATE TABLE invoices (
    id       bigint PRIMARY KEY,
    total    numeric(28, 9) NOT NULL,
    currency char(3)        NOT NULL
);
```

Write `Number()` and `Currency()`. Read the number as a string and the currency as a `currency.Currency`, and build the amount with `NewMoneyFromString`:

```go
_, err := db.Exec(`INSERT INTO invoices (id, total, currency) VALUES ($1, $2, $3)`, id, total.Number(), total.Currency())

var number string
var c currency.Currency
err = db.QueryRow(`SELECT total, currency FROM invoices WHERE id = $1`, id).Scan(&number, &c)
total, err := money.NewMoneyFromString(number, c)
```

Scan the number into a `string`, never a `float64`. `numeric(28, 9)` holds the full range and all 9 decimal places.

`Money` is deliberately not a `driver.Valuer` or `sql.Scanner`: those fill a single column, which would make one text column the easy path.

### Reading back custom currencies

Text and JSON decoding look the currency up by its code, which works for ISO 4217 currencies only: a code alone does not say how many decimal places `BTC` has. An application that stores amounts of custom currencies reads the number and the code itself, as in the two-column example with the code scanned into a string, finds the currency in its own list, and calls `NewMoneyFromString`.

## Zero Value

An amount is built by a constructor, but Go still lets anyone write `money.Money{}`. That value has no currency, so it is not money. It stands for a missing amount: `IsValid` returns false, and other methods are meaningless on it.

Nothing of a currency, `0.00 USD`, is a different and valid value; `IsZero` tests for that.

## Testing

```bash
go test -race -cover ./...
```

## License

MIT License. See [LICENSE](./LICENSE).
