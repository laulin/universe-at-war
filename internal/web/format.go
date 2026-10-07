package web

import (
	"html/template"
	"strconv"
	"strings"
	"time"
)

// templateFuncs are the few helpers the views need. Everything else is computed
// in Go: a template that does arithmetic is a template nobody can test.
var templateFuncs = template.FuncMap{
	"decimal": decimal,
	"emojis": func() []string {
		return []string{"😀", "😂", "😍", "👍", "🚀", "🛰️", "⚔️", "🛡️", "🔥", "🎉"}
	},
	"figure":    figure,
	"instant":   instant,
	"localTime": localTime,
	"signed":    signed,
	"unitName":  unitName,
}

// instant serializes the absolute value consumed by browsers and countdowns.
// It deliberately stays in UTC even when the visible fallback is localized.
func instant(value any) string {
	return timeValue(value).UTC().Format(time.RFC3339Nano)
}

// localTime formats an instant in the universe timezone. JavaScript replaces
// this fallback with the player's browser timezone when it is available.
func localTime(value any, location *time.Location, layout string) string {
	if location == nil {
		location = time.UTC
	}
	return timeValue(value).In(location).Format(layout)
}

func timeValue(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case *time.Time:
		if typed != nil {
			return *typed
		}
	}
	return time.Time{}
}

// decimal renders sampled unit averages in the French notation used by the
// rest of the interface. One decimal keeps rare losses visible instead of
// rounding them down to zero.
func decimal(value float64) string {
	return strings.Replace(strconv.FormatFloat(value, 'f', 1, 64), ".", ",", 1)
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
