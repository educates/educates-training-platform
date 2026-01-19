package portal

import (
	"hash/maphash"
	"math/rand"
	"strings"
)

func RandomPassword(length int) string {
	rand.New(rand.NewSource(int64(new(maphash.Hash).Sum64())))

	chars := []rune("!#%+23456789:=?@ABCDEFGHJKLMNPRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

	var b strings.Builder

	for i := 0; i < length; i++ {
		b.WriteRune(chars[rand.Intn(len(chars))])
	}
	return b.String()
}
