package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/nkall/compactnumber"
	"github.com/pancsta/asyncmachine-go/scripts/shared"
)

func init() {
	shared.GoToRootDir()
}

func main() {
	var input string

	if len(os.Args) > 1 {
		input = os.Args[1]
	} else {
		fmt.Scanln(&input) //nolint:errcheck
	}

	number, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf("Invalid number: %s\n", input)
		return
	}

	formatter := compactnumber.NewFormatter("en-US", compactnumber.Short)
	out, err := formatter.Format(number)
	if err != nil {
		panic(err)
	}

	fmt.Println(out)
}
