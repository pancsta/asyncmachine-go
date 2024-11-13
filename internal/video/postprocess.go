package main

import (
	"image/gif"
	"os"
	"time"
)

func main() {
	// Open the GIF file
	file, err := os.Open("input.gif")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// Decode the GIF
	g, err := gif.DecodeAll(file)
	if err != nil {
		panic(err)
	}

	// Calculate the total duration of the first 100ms
	var totalDuration time.Duration
	for i, delay := range g.Delay {
		frameDuration := time.Duration(delay) * 10 * time.Millisecond
		if totalDuration+frameDuration > 100*time.Millisecond {
			g.Image = g.Image[i:]
			g.Delay = g.Delay[i:]
			break
		}
		totalDuration += frameDuration
	}

	// Encode the remaining frames back into a GIF file
	outFile, err := os.Create("output.gif")
	if err != nil {
		panic(err)
	}
	defer outFile.Close()

	err = gif.EncodeAll(outFile, g)
	if err != nil {
		panic(err)
	}
}
