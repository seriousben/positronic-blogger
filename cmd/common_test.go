package cmd

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestToStringPtr(t *testing.T) {
	sp := ToStringPtr("")
	assert.Assert(t, sp != nil)

}
