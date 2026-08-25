package main

import (
	"context"

	"mymach"
	"mymach/states"
)

var ss = states.MyMachStates

func main() {
	ctx := context.Background()
	h, _ := mymach.New(ctx)
	h.Mach.Add1(ss.Start, nil)
	<-h.Mach.WhenNot1(ss.Start, nil)
}
