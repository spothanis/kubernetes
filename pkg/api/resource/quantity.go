/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resource

import (
	"errors"
	"math/big"
	"regexp"
	"strings"

	"speter.net/go/exp/math/dec/inf"
)

// Quantity is a fixed-point representation of a number.
// It provides convenient marshaling/unmarshaling in JSON and YAML,
// in addition to String() and Int64() accessors.
//
// The serialization format is:
//
// <serialized>      ::= <sign><numeric> | <numeric>
// <numeric>         ::= <digits><suffix> | <digits>.<digits><suffix>
//   (Note that <suffix> may be ""!)
// <sign>            ::= "+" | "-"
// <digits>          ::= <digit> | <digit><digits>
// <signedDigits>    ::= <digits> | <sign><digits>
// <digit>           ::= 0 | 1 | ... | 9
// <suffix>          ::= <binarySuffix> | <decimalExponent> | <decimalSuffix>
// <binarySuffix>    ::= i | Ki | Mi | Gi | Ti | Pi | Ei
// <decimalSuffix>   ::= m | "" | k | M | G | T | P | E
// <decimalExponent> ::= "e" <signedDigits> | "E" <signedDigits>
//   (Note that 1024 = 1Ki but 1000 = 1k; I didn't choose the capitalization.)
//
// No matter which of the three exponent forms is used, no quantity may represent
// a number greater than 2^63-1 in magnitude, nor may it have more than 3 digits
// of precision. Numbers larger or more precise will be capped or rounded.
// (E.g.: 0.1m will rounded up to 1m.)
// This may be extended in the future if we require larger or smaller quantities.
//
// When a Quantity is parsed from a string, it will remember the type of suffix
// it had, and will use the same type again when it is serialized.
// One exception: numbers with a Binary SI suffix less than one will be changed
// to Decimal SI suffix. E.g., .5i becomes 500m. [NOT 512m!]
//
// Before serializing, Quantity will be put in "canonical form".
// This means that Exponent/suffix will be adjusted up or down (with a
// corresponding increase or decrease in Mantissa) such that:
//   a. No precision is lost
//   b. No fractional digits will be emitted
//   c. The exponent (or suffix) is as large as possible.
// The sign will be omitted unless the number is negative.
//
// Examples:
//   1.5 will be serialized as "1500m"
//   1.5Gi will be serialized as "1576Mi"
//
// Note that the quantity will NEVER be internally represented by a
// floating point number. That is the whole point of this exercise.
//
// Non-canonical values will still parse as long as they are well formed,
// but will be re-emitted in their canonical form. (So always use canonical
// form, or don't diff.)
//
// This format is intended to make it difficult to use these numbers without
// writing some sort of special handling code in the hopes that that will
// cause implementors to also use a fixed point implementation.
type Quantity struct {
	// Amount is public, so you can manipulate it if the accessor
	// functions are not sufficient.
	Amount *inf.Dec

	// Change Format at will. See the comment for Canonicalize for
	// more details.
	Format
}

// Format lists the three possible formattings of a quantity.
type Format string

const (
	DecimalExponent = Format("DecExponent")
	// SI = International System of units.
	BinarySI  = Format("BinSI")
	DecimalSI = Format("DecSI")
)

const (
	// splitREString is used to separate a number from its suffix; as such,
	// this is overly permissive, but that's OK-- it will be checked later.
	splitREString = "^([+-]?[0123456789.]+)([eEimkKMGTP]*[-+]?[0123456789]*)$"
)

var (
	// splitRE is used to get the various parts of a number.
	splitRE = regexp.MustCompile(splitREString)

	// Errors that could happen while parsing a string.
	ErrFormatWrong = errors.New("quantities must match the regular expression '" + splitREString + "'")
	ErrNumeric     = errors.New("unable to parse numeric part of quantity")
	ErrSuffix      = errors.New("unable to parse quantity's suffix")

	// Commonly needed big.Int values-- treat as read only!
	ten      = big.NewInt(10)
	zero     = big.NewInt(0)
	one      = big.NewInt(1)
	thousand = big.NewInt(1000)
	ten24    = big.NewInt(1024)

	// Commonly needed inf.Dec values-- treat as read only!
	decZero     = inf.NewDec(0, 0)
	decOne      = inf.NewDec(1, 0)
	decMinusOne = inf.NewDec(-1, 0)
	decThousand = inf.NewDec(1000, 0)

	// Smallest and largest (in magnitude) numbers allowed.
	minAllowed = inf.NewDec(1, 3)         // == 1/1000
	maxAllowed = inf.NewDec((1<<63)-1, 0) // == max int64

	// The maximum value we can represent milli-units for.
	// Compare with the return value of Quantity.Value() to
	// see if it's safe to use Quantity.MilliValue().
	MaxMilliValue = ((1 << 63) - 1) / 1000
)

// ParseQuantity turns str into a Quantity, or returns an error.
func ParseQuantity(str string) (*Quantity, error) {
	parts := splitRE.FindStringSubmatch(strings.TrimSpace(str))
	if len(parts) != 3 {
		return nil, ErrFormatWrong
	}

	amount := new(inf.Dec)
	if _, ok := amount.SetString(parts[1]); !ok {
		return nil, ErrNumeric
	}

	base, exponent, format, ok := quantitySuffixer.interpret(suffix(parts[2]))
	if !ok {
		return nil, ErrSuffix
	}

	// So that no one but us has to think about suffixes, remove it.
	if base == 10 {
		amount.SetScale(amount.Scale() + inf.Scale(-exponent))
	} else if base == 2 {
		// numericSuffix = 2 ** exponent
		numericSuffix := big.NewInt(1).Lsh(one, uint(exponent))
		amount.UnscaledBig().Mul(amount.UnscaledBig(), numericSuffix)
	}

	// Cap at min/max bounds.
	sign := amount.Sign()
	if sign == -1 {
		amount.Neg(amount)
	}
	// This rounds non-zero values up to the minimum representable
	// value, under the theory that if you want some resources, you
	// should get some resources, even if you asked for way too small
	// of an amount.
	// Arguably, this should be inf.RoundHalfUp (normal rounding), but
	// that would have the side effect of rounding values < .5m to zero.
	amount.Round(amount, 3, inf.RoundUp)

	// The max is just a simple cap.
	if amount.Cmp(maxAllowed) > 0 {
		amount.Set(maxAllowed)
	}
	if format == BinarySI && amount.Cmp(decOne) < 0 && amount.Cmp(decZero) > 0 {
		// This avoids rounding and hopefully confusion, too.
		format = DecimalSI
	}
	if sign == -1 {
		amount.Neg(amount)
	}

	return &Quantity{amount, format}, nil
}

// removeFactors divides in a loop; the return values have the property that
// d == result * factor ^ times
// d may be modified in place.
// If d == 0, then the return values will be (0, 0)
func removeFactors(d, factor *big.Int) (result *big.Int, times int) {
	q := big.NewInt(0)
	m := big.NewInt(0)
	for d.Cmp(zero) != 0 {
		q.DivMod(d, factor, m)
		if m.Cmp(zero) != 0 {
			break
		}
		times++
		d, q = q, d
	}
	return d, times
}

// Canonicalize returns the canonical form of q and its suffix (see comment on Quantity).
//
// Note about BinarySI:
// * If q.Format is set to BinarySI and q.Amount represents a non-zero value between
//   -1 and +1, it will be emitted as if q.Format were DecimalSI.
// * Otherwise, if q.Format is set to BinarySI, frational parts of q.Amount will be
//   rounded up. (1.1i becomes 2i.)
func (q *Quantity) Canonicalize() (string, suffix) {
	if q.Amount == nil {
		return "0", ""
	}

	format := q.Format
	switch format {
	case DecimalExponent, DecimalSI:
	case BinarySI:
		switch q.Amount.Cmp(decZero) {
		case 0: // exactly equal 0, that's fine
		case 1: // greater than 0
			if q.Amount.Cmp(decOne) < 0 {
				// This avoids rounding and hopefully confusion, too.
				format = DecimalSI
			}
		case -1:
			if q.Amount.Cmp(decMinusOne) > 0 {
				// This avoids rounding and hopefully confusion, too.
				format = DecimalSI
			}
		}
	default:
		format = DecimalExponent
	}

	// TODO: If BinarySI formatting is requested but would cause rounding, upgrade to
	// one of the other formats.
	switch format {
	case DecimalExponent, DecimalSI:
		mantissa := q.Amount.UnscaledBig()
		exponent := int(-q.Amount.Scale())
		amount := big.NewInt(0).Set(mantissa)
		// move all factors of 10 into the exponent for easy reasoning
		amount, times := removeFactors(amount, ten)
		exponent += times

		// make sure exponent is a multiple of 3
		for exponent%3 != 0 {
			amount.Mul(amount, ten)
			exponent--
		}

		suffix, _ := quantitySuffixer.construct(10, exponent, format)
		number := amount.String()
		return number, suffix
	case BinarySI:
		tmp := &inf.Dec{}
		//tmp.Set(q.Amount)
		tmp.Round(q.Amount, 0, inf.RoundUp)
		amount := tmp.UnscaledBig()
		exponent := int(-q.Amount.Scale())
		// Apply the (base-10) shift. This will lose any fractional
		// part, which is intentional.
		for exponent > 0 {
			amount.Mul(amount, ten)
			exponent--
		}

		amount, exponent = removeFactors(amount, ten24)
		suffix, _ := quantitySuffixer.construct(2, exponent*10, format)
		number := amount.String()
		return number, suffix
	}
	return "0", ""
}

// String formats the Quantity as a string.
func (q *Quantity) String() string {
	number, suffix := q.Canonicalize()
	return number + string(suffix)
}

// MarshalJSON implements the json.Marshaller interface.
func (q Quantity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + q.String() + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaller interface.
func (q *Quantity) UnmarshalJSON(value []byte) error {
	str := string(value)
	parsed, err := ParseQuantity(strings.Trim(str, `"`))
	if err != nil {
		return err
	}
	// This copy is safe because parsed will not be referred to again.
	*q = *parsed
	return nil
}

// NewQuantity returns a new Quantity representing the given
// value in the given format.
func NewQuantity(value int64, format Format) *Quantity {
	return &Quantity{
		Amount: inf.NewDec(value, 0),
		Format: format,
	}
}

// NewMilliQuantity returns a new Quantity representing the given
// value * 1/1000 in the given format. Note that BinarySI formatting
// will cause rounding for fractional values.
func NewMilliQuantity(value int64, format Format) *Quantity {
	return &Quantity{
		Amount: inf.NewDec(value, 3),
		Format: format,
	}
}

// Value returns the value of q; any fractional part will be lost.
func (q *Quantity) Value() int64 {
	if q.Amount == nil {
		return 0
	}
	tmp := &inf.Dec{}
	return tmp.Round(q.Amount, 0, inf.RoundUp).UnscaledBig().Int64()
}

// MilliValue returns the value of q * 1000; this could overflow an int64;
// if that's a concern, call Value() first to verify the number is small enough.
func (q *Quantity) MilliValue() int64 {
	if q.Amount == nil {
		return 0
	}
	tmp := &inf.Dec{}
	return tmp.Round(tmp.Mul(q.Amount, decThousand), 0, inf.RoundUp).UnscaledBig().Int64()
}

// Set sets q's value to be value.
func (q *Quantity) Set(value int64) {
	if q.Amount == nil {
		q.Amount = &inf.Dec{}
	}
	q.Amount.SetUnscaled(value)
	q.Amount.SetScale(0)
}

// SetMilli sets q's value to be value * 1/1000.
func (q *Quantity) SetMilli(value int64) {
	if q.Amount == nil {
		q.Amount = &inf.Dec{}
	}
	q.Amount.SetUnscaled(value)
	q.Amount.SetScale(3)
}

// Copy is a convenience function that makes a deep copy for you. Non-deep
// copies of quantities share pointers and you will regret that.
func (q *Quantity) Copy() *Quantity {
	if q.Amount == nil {
		return NewQuantity(0, q.Format)
	}
	tmp := &inf.Dec{}
	return &Quantity{
		Amount: tmp.Set(q.Amount),
		Format: q.Format,
	}
}
