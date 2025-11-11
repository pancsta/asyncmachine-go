/*
# Schema

Flight1GoToGate

Status:
	Flight1OnTime
	Flight1Delayed
	Flight1Departed
	Flight1Arrived
	Flight1Scheduled

Direction:
	Flight1Inbound
	Flight1Outbound

Gates:
	Flight1GateUnknown
	Flight1Gate1
	Flight1Gate2
	Flight1Gate3
*/

package main

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/pancsta/asyncmachine-go/tools/generator"
	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

const (
	flights = 10
	gates   = 5
)

func main() {
	ctx := context.Background()

	// TODO am.Schema to cli.SFParams converter
	params := cli.SFParams{
		Name:    "Flights",
		Inherit: "rpc/worker,basic",
	}

	for i := 1; i <= flights; i++ {
		numF := strconv.Itoa(i)
		flight := "Flight" + numF
		params.States += ","
		params.Groups += ","

		// Status
		params.States += flight + "OnTime:remove(_" + flight + "Status)," +
			flight + "Delayed:remove(_" + flight + "Status)," +
			flight + "Departed:remove(_" + flight + "Status;" + flight + "GoToGate)," +
			flight + "Arrived:remove(_" + flight + "Status)," +
			flight + "Scheduled:auto:remove(_" + flight + "Status)," +
			// Direction
			flight + "Inbound:remove(_" + flight + "Direction)," +
			flight + "Outbound:remove(_" + flight + "Direction)," +
			// Gates
			flight + "GoToGate:require(" + flight + "Outbound)," +
			flight + "GateUnknown:auto,"

		// Direction
		params.Groups += flight + "Direction(" +
			flight + "Inbound;" + flight + "Outbound),"

		// Status
		params.Groups += flight + "Status(" +
			flight + "OnTime;" + flight + "Delayed;" + flight + "Departed;" + flight + "Arrived;" + flight + "Scheduled),"

		// Gates
		params.Groups += flight + "Gates("
		for ii := 1; ii <= gates; ii++ {
			numG := strconv.Itoa(ii)
			gate := flight + "Gate" + numG

			params.States += gate + ":remove(_" + flight + "Gates),"
			params.Groups += gate + ";"
		}
		params.Groups = strings.TrimRight(params.Groups, ";") + "),"
	}

	gen, err := generator.NewSchemaGenerator(ctx, params)
	if err != nil {
		panic(err)
	}

	// save to ../states/ss_random_data.go
	out := gen.Output()
	err = os.WriteFile("../states/ss_flights.go", []byte(out), 0644)
	if err != nil {
		panic(err)
	}
}
