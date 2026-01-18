package topology

import (
	"math/rand"
	"time"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandomDNSName returns a random string that is a valid DNS name.
func RandomDNSName(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, n)
	for i := range b {
		if i == 0 {
			b[i] = letterBytes[rand.Intn(25)]
		} else {
			b[i] = letterBytes[rand.Intn(len(letterBytes))]
		}
	}
	return string(b)
}
