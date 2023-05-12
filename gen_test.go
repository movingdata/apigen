package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplit(t *testing.T) {
	for _, testCase := range []struct {
		input string
		words []string
	}{
		{"OneTwoThree", []string{"One", "Two", "Three"}},
		{"FNNAPIUser", []string{"FNN", "API", "User"}},
		{"WBISIM", []string{"WBI", "SIM"}},
	} {
		t.Run(testCase.input, func(t *testing.T) {
			assert.Equal(t, testCase.words, splitWords(testCase.input))
		})
	}
}
