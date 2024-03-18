package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	usage := "Usage: am-gen states-file State1,State2"

	// require 1st and 2nd args
	if len(os.Args) < 3 {
		fmt.Println(usage)
		os.Exit(1)
	}

	states := strings.Split(os.Args[2], ",")
	if len(states) == 0 {
		fmt.Println(usage)
		os.Exit(1)
	}

	fmt.Print(generator.GenStatesFile(states))
}
