package tuning

import (
	"strconv"
	"strings"
	"time"
)

func parseBoolLoose(s string) (bool, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, false
	}
	s = strings.ToLower(s)
	switch s {
	case "true", "t", "1", "yes", "y", "on":
		return true, true
	case "false", "f", "0", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func parseInt64Base10(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

func parseFloat64(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(strings.TrimSpace(s))
}
