// Package sample is a fixture for GoSegmenter's golden test — a small
// but representative file: a doc comment, an import block, a type, a
// package-level var, and two funcs (one with a doc comment).
package sample

import (
	"fmt"
	"strings"
)

// Greeting is a configurable greeting message.
type Greeting struct {
	Name string
}

var defaultGreeting = Greeting{Name: "world"}

// String returns the rendered greeting.
func (g Greeting) String() string {
	return fmt.Sprintf("hello, %s", g.Name)
}

func shout(s string) string {
	return strings.ToUpper(s)
}
