package main

import (
	"testing"
)

// simple check for panics
func TestAll(t *testing.T) {
	FooBar()
	FileProcessed()
	DryWaterWet()
	RemoveByAdd()
	AddOptionalRemoveMandatory()
	Mutex()
	Quiz()
}
