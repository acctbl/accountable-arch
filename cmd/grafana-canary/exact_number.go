package main

import (
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

const maximumDecimalExponent = 1000

var exactNumberPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

type exactNumber struct {
	value big.Rat
}

func parseExactNumber(input string) (exactNumber, error) {
	if !exactNumberPattern.MatchString(input) {
		return exactNumber{}, errors.New("the capacity value is invalid")
	}
	mantissa, exponent, err := splitDecimalExponent(input)
	if err != nil {
		return exactNumber{}, err
	}
	parsed, ok := new(big.Rat).SetString(mantissa)
	if !ok || parsed.Sign() < 0 {
		return exactNumber{}, errors.New("the capacity value is invalid")
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(exponent))), nil)
	if exponent >= 0 {
		parsed.Mul(parsed, new(big.Rat).SetInt(scale))
	} else {
		parsed.Quo(parsed, new(big.Rat).SetInt(scale))
	}
	return exactNumber{value: *parsed}, nil
}

func splitDecimalExponent(input string) (string, int, error) {
	separator := strings.IndexAny(input, "eE")
	if separator < 0 {
		return input, 0, nil
	}
	if strings.ContainsAny(input[separator+1:], "eE") {
		return "", 0, errors.New("the capacity exponent is invalid")
	}
	exponent, err := strconv.Atoi(input[separator+1:])
	if err != nil || exponent < -maximumDecimalExponent || exponent > maximumDecimalExponent {
		return "", 0, errors.New("the capacity exponent is invalid")
	}
	return input[:separator], exponent, nil
}

func mustExactNumber(input string) exactNumber {
	value, err := parseExactNumber(input)
	if err != nil {
		panic(err)
	}
	return value
}

func (number *exactNumber) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		return errors.New("the capacity value cannot be null")
	}
	parsed, err := parseExactNumber(string(raw))
	if err != nil {
		return err
	}
	*number = parsed
	return nil
}

func (number exactNumber) Sign() int {
	return number.value.Sign()
}

func (number exactNumber) Compare(other exactNumber) int {
	return number.value.Cmp(&other.value)
}

func (number exactNumber) Add(other exactNumber) exactNumber {
	return exactNumber{value: *new(big.Rat).Add(&number.value, &other.value)}
}

func (number exactNumber) Subtract(other exactNumber) exactNumber {
	return exactNumber{value: *new(big.Rat).Sub(&number.value, &other.value)}
}

func (number exactNumber) Multiply(other exactNumber) exactNumber {
	return exactNumber{value: *new(big.Rat).Mul(&number.value, &other.value)}
}

func (number exactNumber) Divide(other exactNumber) (exactNumber, error) {
	if other.Sign() == 0 {
		return exactNumber{}, errors.New("the capacity divisor is zero")
	}
	return exactNumber{value: *new(big.Rat).Quo(&number.value, &other.value)}, nil
}

func (number exactNumber) IsInteger() bool {
	return number.value.IsInt()
}

func (number exactNumber) String() string {
	if number.value.IsInt() {
		return number.value.Num().String()
	}
	return strings.TrimRight(strings.TrimRight(number.value.FloatString(18), "0"), ".")
}

func minimumExact(left, right exactNumber) exactNumber {
	if left.Compare(right) <= 0 {
		return left
	}
	return right
}

func maximumExact(left, right exactNumber) exactNumber {
	if left.Compare(right) >= 0 {
		return left
	}
	return right
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
