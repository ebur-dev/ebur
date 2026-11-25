package project

import (
	"fmt"
	"strconv"
	"strings"
)

// version is major.minor.patch; missing parts are zero.
type version [3]int

func parseVersion(s string) (version, int, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+@"); i >= 0 {
		s = s[:i] // ignore stability flags such as -dev, @stable
	}
	var v version
	parts := strings.Split(s, ".")
	if s == "" || len(parts) > 4 {
		return v, 0, fmt.Errorf("invalid version %q", s)
	}
	n := 0
	for i, p := range parts {
		if p == "*" || p == "x" || p == "X" {
			break
		}
		if i >= 3 {
			break // a fourth part (8.2.1.0) does not change PHP matching
		}
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return v, 0, fmt.Errorf("invalid version %q", s)
		}
		v[i] = x
		n++
	}
	return v, n, nil
}

func cmp(a, b version) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Constraint reports whether a PHP version satisfies a Composer-style
// version constraint.
type Constraint func(v version) bool

// ParseConstraint understands the Composer syntax projects use for
// "require": {"php": ...}: ^ and ~, comparison operators, wildcards,
// hyphen ranges, AND (space or comma) and OR (| or ||).
func ParseConstraint(s string) (Constraint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty constraint")
	}
	var anyOf []Constraint
	for _, alt := range strings.Split(strings.ReplaceAll(s, "||", "|"), "|") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			return nil, fmt.Errorf("invalid constraint %q", s)
		}
		c, err := parseAnd(alt)
		if err != nil {
			return nil, err
		}
		anyOf = append(anyOf, c)
	}
	return func(v version) bool {
		for _, c := range anyOf {
			if c(v) {
				return true
			}
		}
		return false
	}, nil
}

func parseAnd(s string) (Constraint, error) {
	// Hyphen range: "8.1 - 8.3" means >=8.1.0 and <8.4.0 (partial upper
	// bound includes the whole series, as in Composer).
	if lo, hi, ok := strings.Cut(s, " - "); ok {
		l, _, err := parseVersion(lo)
		if err != nil {
			return nil, err
		}
		h, n, err := parseVersion(hi)
		if err != nil {
			return nil, err
		}
		upper := func(v version) bool { return cmp(v, h) <= 0 }
		if n < 3 {
			next := bump(h, n)
			upper = func(v version) bool { return cmp(v, next) < 0 }
		}
		return func(v version) bool { return cmp(v, l) >= 0 && upper(v) }, nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' })
	// Join operators separated from their version: ">= 8.1" -> ">=8.1".
	var terms []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if strings.Trim(f, "<>=!^~") == "" && i+1 < len(fields) {
			f += fields[i+1]
			i++
		}
		terms = append(terms, f)
	}
	var all []Constraint
	for _, t := range terms {
		c, err := parseTerm(t)
		if err != nil {
			return nil, err
		}
		all = append(all, c)
	}
	return func(v version) bool {
		for _, c := range all {
			if !c(v) {
				return false
			}
		}
		return true
	}, nil
}

// bump returns the first version after the series named by the first n
// parts: bump(8.2, 2) = 8.3.0, bump(8, 1) = 9.0.0.
func bump(v version, n int) version {
	var out version
	if n <= 0 {
		return version{1 << 30, 0, 0}
	}
	copy(out[:n], v[:n])
	out[n-1]++
	return out
}

func parseTerm(t string) (Constraint, error) {
	op := ""
	for _, p := range []string{">=", "<=", "!=", "==", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(t, p) {
			op, t = p, t[len(p):]
			break
		}
	}
	if t == "*" || t == "x" {
		return func(version) bool { return true }, nil
	}
	v, n, err := parseVersion(t)
	if err != nil {
		return nil, err
	}
	ge := func(v2 version) bool { return cmp(v2, v) >= 0 }
	switch op {
	case ">=":
		return ge, nil
	case ">":
		return func(x version) bool { return cmp(x, v) > 0 }, nil
	case "<=":
		return func(x version) bool { return cmp(x, v) <= 0 }, nil
	case "<":
		return func(x version) bool { return cmp(x, v) < 0 }, nil
	case "!=":
		return func(x version) bool { return cmp(x, v) != 0 }, nil
	case "^":
		// ^8.2 -> >=8.2.0 <9.0.0; ^0.3 -> <0.4.0 (first non-zero part fixed).
		lead := 0
		for lead < 2 && v[lead] == 0 && lead < n-1 {
			lead++
		}
		next := bump(v, lead+1)
		return func(x version) bool { return ge(x) && cmp(x, next) < 0 }, nil
	case "~":
		// ~8.2 -> >=8.2 <9.0; ~8.2.1 -> >=8.2.1 <8.3.0.
		keep := n - 1
		if keep < 1 {
			keep = 1
		}
		next := bump(v, keep)
		return func(x version) bool { return ge(x) && cmp(x, next) < 0 }, nil
	}
	// No operator or "=": a partial version such as "8.2" or "8.2.*"
	// means that whole series; a full one means exactly that version.
	if n < 3 {
		next := bump(v, n)
		return func(x version) bool { return ge(x) && cmp(x, next) < 0 }, nil
	}
	return func(x version) bool { return cmp(x, v) == 0 }, nil
}

// Satisfies reports whether version string v meets constraint c.
func Satisfies(c Constraint, v string) bool {
	pv, _, err := parseVersion(v)
	return err == nil && c(pv)
}
