package client

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses a duration like time.ParseDuration but additionally
// accepts a "d" (days) suffix, e.g. "30d" = 720h. The result must be
// positive.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("empty duration")
	}
	var d time.Duration
	if strings.HasSuffix(s, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (want e.g. 30d or 720h)", s)
		}
		d = time.Duration(days * 24 * float64(time.Hour))
	} else {
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (want e.g. 30d or 720h)", s)
		}
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %q", s)
	}
	return d, nil
}
