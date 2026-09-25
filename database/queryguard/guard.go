// Package queryguard bounds historical queries before loading records into memory.
package queryguard

import (
	"context"
	"errors"
	"time"
)

const MaxHours = 366 * 24
const MaxRows = 200000

var ErrTooLarge = errors.New("too many records; select a shorter time range or a single node")
var slots = make(chan struct{}, 4)

func Acquire() bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}
func Release() { <-slots }
func ValidRange(start, end time.Time) bool {
	return !end.Before(start) && end.Sub(start) <= MaxHours*time.Hour
}
func Context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
