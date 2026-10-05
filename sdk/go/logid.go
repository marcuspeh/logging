package loggingsdk

import (
	"math/rand/v2"
	"time"
)

const (
	logIDAlphabet   = "0123456789abcdefghijklmnopqrstuvwxyz"
	logIDPostfixLen = 6
	logIDTimeLayout = "20060102-1504"
)

// NewLogID returns a fresh correlation id shaped
// "yyyymmdd-hhmm-postfix", e.g. "20261005-1430-k3f9qz".
//
// The datetime prefix is UTC, matching the event timestamps the query
// UI renders, so an id alone tells you when a burst of logs happened
// and sorts chronologically as a string. The random postfix keeps ids
// unique within the same minute. Pass the result to WithLogID.
func NewLogID() string {
	b := make([]byte, logIDPostfixLen)
	for i := range b {
		b[i] = logIDAlphabet[rand.IntN(len(logIDAlphabet))]
	}
	return time.Now().UTC().Format(logIDTimeLayout) + "-" + string(b)
}
