package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/v60/github"
)

func main() {
	changelog, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		panic(err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		panic("GITHUB_TOKEN environment variable is required")
	}

	client := github.NewClient(nil).WithAuthToken(token)
	ctx := context.Background()

	// Regex for PR extraction
	prRegex := regexp.MustCompile(`\[\\?#(\d+)\]\(https://github\.com/([^/]+)/([^/]+)/pull/\d+\)`)

	// Regex to extract tag, owner, and repo from the release header
	releaseRegex := regexp.MustCompile(`^## \[(v[^\]]+)\]\(https://github\.com/([^/]+)/([^/]+)/releases/tag/`)

	lines := strings.Split(string(changelog), "\n")
	var currentEntry string
	var inEntry bool

	doc := ""

	const outPath = "../docs/release-notes.md"
	const saveEveryPRs = 10
	prsFetched := 0

	// save writes the (possibly partial) notes to disk
	save := func(reason string) {
		err := os.WriteFile(outPath, []byte(doc), 0644)
		if err != nil {
			panic(err)
		}
		fmt.Printf("SAVED %s (%d bytes, %d PRs fetched): %s\n",
			outPath, len(doc), prsFetched, reason)
	}

	flushEntry := func() {
		if currentEntry == "" {
			return
		}

		// Use Index to find the exact cut-off point for the PR string
		loc := prRegex.FindStringSubmatchIndex(currentEntry)
		if loc != nil {
			prNum, _ := strconv.Atoi(currentEntry[loc[2]:loc[3]])
			owner := currentEntry[loc[4]:loc[5]]
			repo := currentEntry[loc[6]:loc[7]]

			fmt.Printf("Fetching PR %s/%s#%d...\n", owner, repo, prNum)
			pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNum)
			if err != nil {
				log.Printf("Warning: Failed to fetch PR #%d: %v", prNum, err)
			}
			prsFetched++

			// Extract everything before the PR link (ignoring the "- " prefix)
			desc := strings.TrimSpace(currentEntry[2:loc[0]])

			// Extract the PR link and anything trailing it (like the author)
			prSuffix := strings.TrimSpace(currentEntry[loc[0]:])

			// Format the new split header
			doc += fmt.Sprintf("### %s\n\nPR: %s\n\n", desc, prSuffix)

			body := pr.GetBody()
			if body != "" {
				body = strings.TrimSpace(body)
				body = regexp.MustCompile(`(?m)^#`).ReplaceAllString(body, "####")
				doc += fmt.Sprintf("%s\n\n", body)
			} else {
				doc += fmt.Sprintf("_No PR description provided._\n\n")
			}
		} else {
			if strings.HasPrefix(currentEntry, "- ") {
				doc += fmt.Sprintf("### %s\n\n", currentEntry[2:])
			} else {
				doc += fmt.Sprintf("%s\n", currentEntry)
			}
		}

		currentEntry = ""
		inEntry = false

		if prsFetched > 0 && prsFetched%saveEveryPRs == 0 {
			save("periodic")
		}
	}

	doc += fmt.Sprintf("# Release Notes\n\n- [CHANGELOG.md](/CHANGELOG.md)\n\n")
	started := false

	for i, line := range lines {
		if i > 0 && i%10 == 0 {
			fmt.Printf("LINE %d: %s\n", i, line)
		}
		if strings.HasPrefix(line, "## [v") {
			started = true
		}
		if !started {
			continue
		}

		if strings.HasPrefix(line, "## ") {
			flushEntry()
			save("before new release section")
			fmt.Printf("Processing release: %s\n", line)
			doc += fmt.Sprintf("---\n\n%s\n\n", line)

			// Fetch and inject release description
			relMatch := releaseRegex.FindStringSubmatch(line)
			if relMatch != nil {
				tag := relMatch[1]
				owner := relMatch[2]
				repo := relMatch[3]

				fmt.Printf("Fetching release %s/%s %s...\n", owner, repo, tag)
				release, _, err := client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
				if err != nil {
					log.Printf("Warning: Failed to fetch Release %s: %v", tag, err)
				} else if release.GetBody() != "" {
					body := strings.TrimSpace(release.GetBody())
					// remove nested changelogs
					body = regexp.MustCompile("(?s)## Changelog.+").ReplaceAllString(body, "")
					doc += fmt.Sprintf("%s\n\n", body)
				}
			}

		} else if strings.HasPrefix(line, "- ") {
			flushEntry()
			currentEntry = line
			inEntry = true
		} else if inEntry && strings.TrimSpace(line) != "" {
			currentEntry += " " + strings.TrimSpace(line)
		} else if strings.TrimSpace(line) == "" {
			flushEntry()
		} else {
			flushEntry()
			doc += fmt.Sprintf("%s\n", line)
		}
	}

	flushEntry()

	save("final")
	fmt.Println("Done.")
}
