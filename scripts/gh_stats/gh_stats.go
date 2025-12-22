package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/google/go-github/v66/github"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
)

// Config
const (
	csvFilename = "data/repo_stats.csv"
	owner       = "pancsta"         // e.g. "google"
	repo        = "asyncmachine-go" // e.g. "go-github"
)

var token = ""

// DailyStat represents one row in our CSV
type DailyStat struct {
	Date             string
	Views            int
	UniqueViews      int
	Clones           int
	UniqueClones     int
	ReleaseDownloads int // Snapshot of total downloads on this date
}

func init() {
	godotenv.Load()
	token = os.Getenv("GITHUB_TOKEN_STATS")
}

func main() {
	ctx := context.Background()
	getStats(ctx)
	genCharts(ctx)
	render("light", "white")
	render("dark", "#100c2a")
}

// ///// ///// /////

// ///// STATS

// ///// ///// /////

func getStats(ctx context.Context) {

	// 1. Authenticate
	if token == "YOUR_TOKEN" {
		log.Fatal("Please update the token, owner, and repo constants in the code.")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// 2. Load existing CSV data into a map (Date -> Stat)
	statsMap := loadExistingCSV(csvFilename)
	fmt.Printf("Loaded %d historical records.\n", len(statsMap))

	// 3. Fetch Traffic: Views (Last 14 days)
	fmt.Println("Fetching Views...")
	views, _, err := client.Repositories.ListTrafficViews(ctx, owner, repo, nil)
	if err != nil {
		log.Printf("Error fetching views: %v", err)
	} else {
		for _, v := range views.Views {
			dateStr := v.GetTimestamp().Format("2006-01-02")
			entry := getOrCreate(statsMap, dateStr)
			entry.Views = v.GetCount()
			entry.UniqueViews = v.GetUniques()
		}
	}

	// 4. Fetch Traffic: Clones (Last 14 days)
	fmt.Println("Fetching Clones...")
	clones, _, err := client.Repositories.ListTrafficClones(ctx, owner, repo, nil)
	if err != nil {
		log.Printf("Error fetching clones: %v", err)
	} else {
		for _, c := range clones.Clones {
			dateStr := c.GetTimestamp().Format("2006-01-02")
			entry := getOrCreate(statsMap, dateStr)
			entry.Clones = c.GetCount()
			entry.UniqueClones = c.GetUniques()
		}
	}

	// 5. Fetch Release Downloads (Snapshot)
	// TODO wrong number (min 2 per each)
	totalDownloads := 0
	// We assign the current total of *all* releases to "Today's" entry
	// fmt.Println("Fetching Release Downloads...")
	// opts := &github.ListOptions{PerPage: 100}
	// for {
	// 	releases, resp, err := client.Repositories.ListReleases(ctx, owner, repo, opts)
	// 	if err != nil {
	// 		log.Printf("Error fetching releases: %v", err)
	// 		break
	// 	}
	// 	for _, r := range releases {
	// 		for _, asset := range r.Assets {
	// 			totalDownloads += asset.GetDownloadCount()
	// 		}
	// 	}
	// 	if resp.NextPage == 0 {
	// 		break
	// 	}
	// 	opts.Page = resp.NextPage
	// }

	// Update Today's record with the cumulative download count
	todayStr := time.Now().Format("2006-01-02")
	todayEntry := getOrCreate(statsMap, todayStr)
	todayEntry.ReleaseDownloads = totalDownloads

	// 6. Save merged data back to CSV
	if err := saveCSV(csvFilename, statsMap); err != nil {
		log.Fatalf("Failed to save CSV: %v", err)
	}

	fmt.Printf("Done! Stats merged and saved to %s\n", csvFilename)
}

// Helper to get an entry from the map or create it if missing
func getOrCreate(m map[string]*DailyStat, date string) *DailyStat {
	if val, ok := m[date]; ok {
		return val
	}
	newItem := &DailyStat{Date: date}
	m[date] = newItem
	return newItem
}

// Reads the existing CSV and returns a map
func loadExistingCSV(csvPath string) map[string]*DailyStat {
	data := make(map[string]*DailyStat)

	if path.Dir(csvPath) != "" {
		err := os.MkdirAll(path.Dir(csvPath), 0755)
		if err != nil {
			panic(err)
		}
	}

	f, err := os.Open(csvPath)
	if os.IsNotExist(err) {
		return data // Return empty map if file doesn't exist
	}
	if err != nil {
		log.Printf("Could not open existing CSV: %v", err)
		return data
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return data
	}

	// Skip header (row 0)
	for i, row := range records {
		if i == 0 || len(row) < 6 {
			continue
		}
		// Parse columns: Date, Views, UniqueViews, Clones, UniqueClones, ReleaseDL
		views, _ := strconv.Atoi(row[1])
		uViews, _ := strconv.Atoi(row[2])
		clones, _ := strconv.Atoi(row[3])
		uClones, _ := strconv.Atoi(row[4])
		dl, _ := strconv.Atoi(row[5])

		data[row[0]] = &DailyStat{
			Date:             row[0],
			Views:            views,
			UniqueViews:      uViews,
			Clones:           clones,
			UniqueClones:     uClones,
			ReleaseDownloads: dl,
		}
	}
	return data
}

// Sorts the map by date and writes to CSV
func saveCSV(filename string, data map[string]*DailyStat) error {
	// 1. Convert map to slice for sorting
	var stats []*DailyStat
	for _, v := range data {
		stats = append(stats, v)
	}

	// 2. Sort by Date
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Date < stats[j].Date
	})

	// 3. Create File
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()

	// 4. Write Header
	header := []string{"Date", "Views", "Unique_Views", "Clones", "Unique_Clones", "Total_Release_Downloads"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// 5. Write Rows
	for _, s := range stats {
		record := []string{
			s.Date,
			strconv.Itoa(s.Views),
			strconv.Itoa(s.UniqueViews),
			strconv.Itoa(s.Clones),
			strconv.Itoa(s.UniqueClones),
			strconv.Itoa(s.ReleaseDownloads),
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}

// ///// ///// /////

// ///// CHARTS

// ///// ///// /////

func genCharts(ctx context.Context) {
	// 1. Read the CSV file
	// Assuming the file is named "repo_stats.csv"
	f, err := os.Open(csvFilename)
	if err != nil {
		log.Fatalf("Unable to read input file: %v", err)
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	records, err := csvReader.ReadAll()
	if err != nil {
		log.Fatalf("Unable to parse file as CSV: %v", err)
	}

	// 2. Process DataLatest
	var dates []string
	var uniqueClones []opts.LineData

	// Loop through records, skipping header (index 0)
	for i, record := range records {
		if i == 0 {
			continue // Skip Header
		}

		// Safety check for column count
		if len(record) < 5 {
			continue
		}

		dateStr := record[0]
		// Skip rows with empty dates (like the ",0,0,0,0,0" row in your example)
		if dateStr == "" {
			continue
		}

		// Parse Unique_Clones (Column Index 4)
		// CSV Format: Date,Views,Unique_Views,Clones,Unique_Clones,Total_Release_Downloads
		val, err := strconv.Atoi(record[4])
		if err != nil {
			log.Printf("Skipping row %d due to invalid number: %v", i, err)
			continue
		}

		dates = append(dates, dateStr)
		uniqueClones = append(uniqueClones, opts.LineData{Value: val})
	}

	themes := []string{"light", "dark"}

	for _, theme := range themes {
		// 3. Create the Chart
		line := charts.NewLine()

		// Set global options (Titles, Legend, Tooltips)
		line.SetGlobalOptions(
			charts.WithInitializationOpts(opts.Initialization{
				Theme: theme, // Optional: Use a nice theme
				Width: "800px",
			}),
			charts.WithTooltipOpts(opts.Tooltip{
				Show:    opts.Bool(true),
				Trigger: "axis",
			}),
			charts.WithXAxisOpts(opts.XAxis{
				Name: "Date",
			}),
			charts.WithYAxisOpts(opts.YAxis{
				Name: "Count",
			}),
		)

		// Set X-Axis and Add Series
		line.SetXAxis(dates).
			AddSeries("Unique Clones", uniqueClones).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth: opts.Bool(true), // Makes the line curved instead of jagged
				}),
				charts.WithAnimationOpts(opts.Animation{
					Animation: opts.Bool(false),
				}),
				charts.WithLabelOpts(opts.Label{
					Show: opts.Bool(true), // Show numbers on the dots
				}),
			)

		// 4. Save to HTML
		outputFile, _ := os.Create("data/stats." + theme + ".html")
		err = line.Render(outputFile)
		if err != nil {
			log.Println(err)
		}
		outputFile.Close()
	}
}

func render(theme, bg string) {
	// 1. Setup the input and output file paths
	inputHTML := "data/stats." + theme + ".html" // Ensure this file exists
	outputImage := "data/stats." + theme + ".png"

	// Get absolute path for the HTML file (required for file:// protocol)
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	fileURL := "file://" + filepath.Join(wd, inputHTML)

	// 2. Create a Chromedp context
	// This starts a headless Chrome instance
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	// 3. Define the tasks
	var imageBuf []byte
	fmt.Println("Capturing screenshot... (this may take a few seconds)")

	err = chromedp.Run(ctx,
		// Navigate to the local HTML file
		chromedp.Navigate(fileURL),

		// Wait for the charts to render.
		// We look for the canvas tag which echarts generates.
		chromedp.WaitVisible("canvas", chromedp.ByQuery),
		chromedp.Evaluate("document.body.style.backgroundColor = '"+bg+"'", nil),

		// IMPORTANT: Echarts has a startup animation.
		// We sleep briefly to ensure the lines are fully drawn before snapping.
		chromedp.Sleep(2*time.Second),

		// Capture the full page (or specific element)
		chromedp.FullScreenshot(&imageBuf, 90),
	)

	if err != nil {
		log.Fatalf("Error taking screenshot: %v", err)
	}

	// 4. Write the image buffer to a file
	if err := os.WriteFile(outputImage, imageBuf, 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Success! Saved image to %s\n", outputImage)
}
