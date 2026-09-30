//go:build race

package utils

// raceEnabled scales timing bounds: the race detector slows code down ~10x.
const raceEnabled = true
