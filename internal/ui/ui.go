// Package ui provides helpers for colored, aligned terminal output. Colors are
// disabled automatically when the output is not a terminal or when NO_COLOR /
// CAUR_NO_COLOR is set.
package ui

import (
	"os"
	"strconv"
	"strings"
)

// Color enables or disables ANSI sequences (detected on stderr).
var Color = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("CAUR_NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// ANSI codes.
const (
	reset     = "\033[0m"
	codeBold  = "\033[1m"
	codeDim   = "\033[2m"
	codeRed   = "\033[31m"
	codeGreen = "\033[32m"
	codeYell  = "\033[33m"
	codeBlue  = "\033[34m"
	codeCyan  = "\033[36m"
	codeGray  = "\033[90m"
)

func paint(code, s string) string {
	if !Color || s == "" {
		return s
	}
	return code + s + reset
}

// Bold, Dim and the color variants return s decorated (no-op when colors are
// disabled).
func Bold(s string) string   { return paint(codeBold, s) }
func Dim(s string) string    { return paint(codeDim, s) }
func Red(s string) string    { return paint(codeRed, s) }
func Green(s string) string  { return paint(codeGreen, s) }
func Yellow(s string) string { return paint(codeYell, s) }
func Blue(s string) string   { return paint(codeBlue, s) }
func Cyan(s string) string   { return paint(codeCyan, s) }
func Gray(s string) string   { return paint(codeGray, s) }

// BoldColor combines bold with a color.
func BoldColor(code, s string) string { return paint(codeBold+code, s) }

// RedBold, etc. are handy shortcuts.
func RedBold(s string) string    { return BoldColor(codeRed, s) }
func GreenBold(s string) string  { return BoldColor(codeGreen, s) }
func YellowBold(s string) string { return BoldColor(codeYell, s) }

// IsTTY reports whether the session is interactive (stdin and stderr are both
// terminals), so caur can prompt and launch interactive sub-programs.
func IsTTY() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stderr)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Width returns the usable terminal width (default 100).
func Width() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(c)); err == nil && n > 20 {
			return n
		}
	}
	return 100
}

// Wrap wraps text into lines at most width long, prefixing each line with
// indent. It breaks on words.
func Wrap(text string, indent string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	limit := width - len(indent)
	if limit < 20 {
		limit = 20
	}
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() > 0 && cur.Len()+1+len(w) > limit {
			lines = append(lines, indent+cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, indent+cur.String())
	}
	return lines
}

// Pad left-aligns s to at least n columns (counting visible characters, not the
// ANSI sequences).
func Pad(s string, n int) string {
	w := visibleLen(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// visibleLen counts characters ignoring ANSI sequences.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\033':
			inEsc = true
		default:
			n++
		}
	}
	return n
}
