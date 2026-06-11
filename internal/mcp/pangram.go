package mcp

import _ "embed"

// The pangram: one annotated example pack that exercises every type and
// construct. It replaces the prose grammar — an agent reads it once and writes
// any pack by analogy. Embedded so it travels in the binary and cannot drift
// from the compiler (it is itself a pack the test suite compiles).
//
//go:embed pangram.kal
var pangramExample string
