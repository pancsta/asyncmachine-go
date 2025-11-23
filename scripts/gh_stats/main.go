package main

import (
	"encoding/csv"
	"fmt"
	"image/color"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

const csvURL = "https://raw.githubusercontent.com/pancsta/asyncmachine-go/refs/heads/github-repo-stats/pancsta/asyncmachine-go/ghrs-data/views_clones_aggregate.csv"

// The CSV data you provided
const csvData = `time_iso8601,clones_total,clones_unique,views_total,views_unique
2025-10-24 00:00:00+00:00,17,12,5,5
2025-10-25 00:00:00+00:00,25,19,5,2
2025-10-26 00:00:00+00:00,17,15,2,2
2025-10-27 00:00:00+00:00,33,25,7,3
2025-10-28 00:00:00+00:00,69,64,11,8
2025-10-29 00:00:00+00:00,15,11,1,1
2025-10-30 00:00:00+00:00,3,3,22,20
2025-10-31 00:00:00+00:00,8,7,2,1
2025-11-01 00:00:00+00:00,21,16,2,1
2025-11-02 00:00:00+00:00,9,9,21,6
2025-11-03 00:00:00+00:00,7,7,0,0
2025-11-04 00:00:00+00:00,6,5,4,2
2025-11-05 00:00:00+00:00,12,12,2,2
2025-11-06 00:00:00+00:00,27,12,2,2`

// A struct to hold our parsed data
type Record struct {
	Date  time.Time
	Value float64
}

// createMockServer creates a local test server to serve the CSV data.
// In a real app, you'd just use your real URL.
func createMockServer() *httptest.Server {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	return server
}

// fetchAndParseData fetches the CSV from the URL and parses it
func fetchAndParseData(csvURL string) ([]Record, error) {
	resp, err := http.Get(csvURL)
	if err != nil {
		return nil, fmt.Errorf("could not fetch URL: %w", err)
	}
	defer resp.Body.Close()

	reader := csv.NewReader(resp.Body)

	// Read the header row
	_, err = reader.Read()
	if err != nil {
		return nil, fmt.Errorf("could not read header: %w", err)
	}

	var records []Record

	// Read all other rows
	for {
		row, err := reader.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("error reading csv row: %w", err)
		}

		// Parse the time (column 0)
		// Layout "2006-01-02 15:04:05Z07:00" matches "2025-10-24 00:00:00+00:00"
		date, err := time.Parse("2006-01-02 15:04:05Z07:00", row[0])
		if err != nil {
			log.Printf("Warning: skipping row, cold not parse date: %v", err)
			continue
		}

		// Parse the value (column 2, "clones_unique")
		value, err := strconv.ParseFloat(row[2], 64)
		if err != nil {
			log.Printf("Warning: skipping row, could not parse value: %v", err)
			continue
		}

		records = append(records, Record{Date: date, Value: value})
	}

	return records, nil
}

// createChart generates the plot and saves it to a file
func createChart(theme string, records []Record, filename string) error {
	p := plot.New()

	p.Title.Text = "Unique Clones Over Time"
	p.X.Label.Text = "Date"
	p.Y.Label.Text = "Unique Clones"

	// Create the data for the plot
	pts := make(plotter.XYs, len(records))
	for i, record := range records {
		pts[i].X = float64(record.Date.Unix()) // Use Unix timestamp for X-axis
		pts[i].Y = record.Value
	}

	// Create the line plot
	line, err := plotter.NewLine(pts)
	if err != nil {
		return err
	}
	line.Width = vg.Points(2)

	// --- Style the plot based on theme ---
	if theme == "dark" {
		// Dark background
		p.BackgroundColor = color.RGBA{R: 34, G: 34, B: 34, A: 255} // #222

		// Light text
		p.Title.TextStyle.Color = color.White
		p.X.Label.TextStyle.Color = color.White
		p.Y.Label.TextStyle.Color = color.White
		p.X.Tick.Label.Color = color.White
		p.Y.Tick.Label.Color = color.White
		p.X.Tick.Color = color.White
		p.Y.Tick.Color = color.White

		// Bright line
		line.Color = color.RGBA{R: 0, G: 160, B: 255, A: 255} // Bright Blue

		// Dark grid
		grid := plotter.NewGrid()
		grid.Vertical.Color = color.White
		grid.Horizontal.Color = color.White
	} else {
		// Light theme (mostly defaults)
		// line.Color = color.RGBA{R: 0, G: 0, B: 200, A: 255}
		p.Add(plotter.NewGrid())
	}

	p.Add(line)

	// Use time ticks for the X-axis
	p.X.Tick.Marker = plot.TimeTicks{Format: "2006-01-02"}

	// Save the plot
	if err := p.Save(8*vg.Inch, 4.5*vg.Inch, filename); err != nil {
		return err
	}

	return nil
}

func main() {
	// Start a mock server for this example
	server := createMockServer()
	defer server.Close()

	// Use the mock server's URL
	// csvURL := server.URL

	// Fetch and parse the data
	records, err := fetchAndParseData(csvURL)
	if err != nil {
		log.Fatalf("Failed to get data: %v", err)
	}
	if len(records) == 0 {
		log.Fatal("No records were parsed from CSV.")
	}

	// Create light mode chart
	if err := createChart("light", records, "chart_light.png"); err != nil {
		log.Fatalf("Failed to create light chart: %v", err)
	}

	// Create dark mode chart
	if err := createChart("dark", records, "chart_dark.png"); err != nil {
		log.Fatalf("Failed to create dark chart: %v", err)
	}

	fmt.Println("Charts generated: chart_light.png, chart_dark.png")
}
