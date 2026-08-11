package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pancsta/asyncmachine-go/scripts/shared"
)

func init() {
	shared.GoToRootDir()
}

// Config holds target repository details
type Config struct {
	Owner string
	Repo  string
	Token string
}

// GitHub API Structs
type Release struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

type PR struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	HTMLURL  string     `json:"html_url"`
	MergedAt *time.Time `json:"merged_at"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
	Base struct {
		Ref string `json:"ref"` // Target branch name (e.g., "main", "dev")
	} `json:"base"`
}

type ReleaseBucket struct {
	Release Release
	PRs     []PR
}

// --- SKIPLIST CONFIGURATION ---

// Skip specific PR numbers
var skiplistNumbers = map[int]bool{
	// 412: true,
}

// Skip PRs if title contains any of these substrings
var skiplistTitles = []string{
	"chore:",
	"docs:",
	"ci:",
	"wip:",
}

// Skip PRs merged into these specific target branches
var skiplistBranches = map[string]bool{
	"dev":     true, // Example: Skip PRs merged into the 'dev' branch
	"preview": true, // Example: Skip PRs merged into a 'preview' branch
}

func main() {
	config := Config{
		Owner: getEnv("REPO_OWNER", "pancsta"),
		Repo:  getEnv("REPO_NAME", "asyncmachine-go"),
		Token: os.Getenv("GITHUB_TOKEN"),
	}

	if config.Token == "" {
		log.Println("Warning: GITHUB_TOKEN environment variable is not set. You may hit API rate limits quickly.")
	}

	// 1. Fetch all releases/tags
	releases := fetchReleases(config)

	// Sort releases descending by publish date (newest first)
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].PublishedAt.After(releases[j].PublishedAt)
	})

	// 2. Fetch all closed pull requests
	prs := fetchClosedPRs(config)

	// 3. Bucket PRs chronologically into their respective releases
	buckets := make([]ReleaseBucket, len(releases))
	for i, r := range releases {
		buckets[i] = ReleaseBucket{Release: r}
	}
	var unreleasedPRs []PR

	for _, pr := range prs {
		if pr.MergedAt == nil {
			continue
		}

		// Filter by PR Number
		if skiplistNumbers[pr.Number] {
			continue
		}

		// Filter by Target Branch Name
		if skiplistBranches[pr.Base.Ref] {
			continue
		}

		// Filter by PR Title Substrings
		skipByTitle := false
		for _, match := range skiplistTitles {
			if strings.Contains(strings.ToLower(pr.Title), strings.ToLower(match)) {
				skipByTitle = true
				break
			}
		}
		if skipByTitle {
			continue
		}

		mergedTime := *pr.MergedAt
		assigned := false

		for i, r := range releases {
			if mergedTime.Before(r.PublishedAt) || mergedTime.Equal(r.PublishedAt) {
				if i == len(releases)-1 || mergedTime.After(releases[i+1].PublishedAt) {
					buckets[i].PRs = append(buckets[i].PRs, pr)
					assigned = true
					break
				}
			}
		}

		if !assigned && len(releases) > 0 && mergedTime.After(releases[0].PublishedAt) {
			unreleasedPRs = append(unreleasedPRs, pr)
		}
	}

	// 4. Generate & Print Markdown Output
	printChangelog(config, buckets, unreleasedPRs)
}

func fetchReleases(config Config) []Release {
	var allReleases []Release
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100&page=%d", config.Owner, config.Repo, page)
		body, err := apiRequest(url, config.Token)
		if err != nil {
			log.Fatalf("Error fetching releases: %v", err)
		}

		var releases []Release
		if err := json.Unmarshal(body, &releases); err != nil {
			log.Fatalf("Error parsing releases JSON: %v", err)
		}

		if len(releases) == 0 {
			break
		}
		allReleases = append(allReleases, releases...)
		page++
	}
	return allReleases
}

func fetchClosedPRs(config Config) []PR {
	var allPRs []PR
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=closed&per_page=100&page=%d", config.Owner, config.Repo, page)
		body, err := apiRequest(url, config.Token)
		if err != nil {
			log.Fatalf("Error fetching PRs: %v", err)
		}

		var prs []PR
		if err := json.Unmarshal(body, &prs); err != nil {
			log.Fatalf("Error parsing PRs JSON: %v", err)
		}

		if len(prs) == 0 {
			break
		}
		allPRs = append(allPRs, prs...)
		page++
	}
	return allPRs
}

func apiRequest(url, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API responded with status: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func escapeMarkdown(title string) string {
	title = strings.ReplaceAll(title, "(", `\(`)
	title = strings.ReplaceAll(title, ")", `\)`)
	return title
}

func printChangelog(config Config, buckets []ReleaseBucket, unreleased []PR) {
	baseRepoURL := fmt.Sprintf("https://github.com/%s/%s", config.Owner, config.Repo)

	fmt.Println("# Changelog")
	fmt.Println()
	fmt.Println("- [Release Notes](/docs/release-notes.md)")
	fmt.Println("- [Breaking Changes](/BREAKING.md)")
	fmt.Printf("- [Repo Traffic](%s/pulse)\n", baseRepoURL)
	fmt.Printf("- [Release Feed](%s/releases.atom)\n", baseRepoURL)
	fmt.Println()

	if len(unreleased) > 0 {
		sortPRsDescending(unreleased)
		fmt.Println("## [Unreleased]")
		fmt.Println()
		for _, pr := range unreleased {
			fmt.Printf("- %s [\\#%d](%s) (@%s)\n", escapeMarkdown(pr.Title), pr.Number, pr.HTMLURL, pr.User.Login)
		}
		fmt.Println()
	}

	for _, b := range buckets {
		if len(b.PRs) == 0 {
			continue
		}

		sortPRsDescending(b.PRs)
		dateStr := b.Release.PublishedAt.Format("2006-01-02")

		fmt.Printf("## [%s](%s) (%s)\n", b.Release.TagName, b.Release.HTMLURL, dateStr)
		fmt.Println()
		for _, pr := range b.PRs {
			fmt.Printf("- %s [\\#%d](%s) (@%s)\n", escapeMarkdown(pr.Title), pr.Number, pr.HTMLURL, pr.User.Login)
		}
		fmt.Println()
	}
}

func sortPRsDescending(prs []PR) {
	sort.Slice(prs, func(i, j int) bool {
		return prs[i].Number > prs[j].Number
	})
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}