package web

import (
	"html/template"
	"strconv"
	"strings"
)

// templateFuncs are the few helpers the views need. Everything else is computed
// in Go: a template that does arithmetic is a template nobody can test.
var templateFuncs = template.FuncMap{
	"figure": figure,
	"signed": signed,
}

// figure groups thousands the way the player reads them in game: 1.659.181.
func figure(value int64) string {
	digits := strconv.FormatInt(value, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var grouped strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}
	return sign + grouped.String()
}

// signed keeps the plus in front of a production rate, which is the difference
// between a stock and a gain.
func signed(value int64) string {
	if value > 0 {
		return "+" + figure(value)
	}
	return figure(value)
}
