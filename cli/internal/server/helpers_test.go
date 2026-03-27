package server

import (
	"strconv"
	"time"
)

func timeNow() time.Time {
	return time.Now().UTC()
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
