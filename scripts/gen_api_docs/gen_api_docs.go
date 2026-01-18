package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var gen = true

// var gen = false

func init() {
	// required by golds
	if err := os.Chdir(".."); err != nil {
		log.Fatalf("Failed to change working directory: %v", err)
	}
}

func main() {
	var err error

	// HTML gen
	if gen {
		fmt.Println("Generating API docs...")
		cmd := exec.Command("golds",
			"-nouses",
			"-only-list-exporteds",
			"-render-doclinks",
			"-theme=dark",
			"-gen",
			"-dir=./docs/api",
			"-source-code-reading", "rich",
			"-wdpkgs-listing", "solo",
			"./...")

		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Fatalf("golds command failed: %v\nOutput: %s", err, output)
		}
		log.Printf("golds completed successfully:\n%s", output)

		// HTML replace
		fmt.Println("Processing HTML files...")
		err = processHTMLFiles("./docs/api")
		if err != nil {
			log.Fatalf("Failed to process HTML files: %v", err)
		}
		fmt.Println("HTML files processed successfully")
	}

	// favicon
	fmt.Println("Generating favicon...")
	faviconCmd := exec.Command("magick",
		"./assets/asyncmachine-go/logo.png",
		"-background", "transparent",
		"-flatten",
		"-define", "icon:auto-resize=256,128,64,48,32,16",
		"./docs/api/favicon.ico")

	faviconOutput, err := faviconCmd.CombinedOutput()
	if err != nil {
		log.Fatalf("favicon generation failed: %v\nOutput: %s", err, faviconOutput)
	}
	log.Printf("Favicon generated successfully:\n%s", faviconOutput)
}

func processHTMLFiles(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".html") {
			if err := replaceInFile(path); err != nil {
				return fmt.Errorf("failed to process %s: %w", path, err)
			}
		}

		return nil
	})
}

// remove repo prefix from Package titles
var titleRegex = regexp.MustCompile(`<title>(\w+:|Source: \w+\.go in package) github\.com/pancsta/asyncmachine-go`)

func replaceInFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// remove repo prefix from main page links
	newContent := strings.ReplaceAll(string(content),
		">github.com/pancsta/asyncmachine-go", ">")

	// inject comments (only for local packages)
	if titleRegex.MatchString(newContent) || path == "docs/api/index.html" {
		if strings.Contains(path, "index.html") {
			print()
		}
		newContent = strings.ReplaceAll(newContent,
			`<pre id="footer">`, `         
				<script src="https://giscus.app/client.js"
					data-repo="pancsta/asyncmachine-go"
					data-repo-id="R_kgDOLDLcZQ"
					data-category="code.asyncmachine.dev"
					data-category-id="DIC_kwDOLDLcZc4C0rDH"
					data-mapping="title"
					data-strict="0"
					data-reactions-enabled="1"
					data-emit-metadata="0"
					data-input-position="top"
					data-theme="dark"
					data-lang="en"
					crossorigin="anonymous"
					async>
 				</script>
				<pre id="footer">`)
	}

	// replace import prefix from <title>
	newContent = titleRegex.ReplaceAllString(newContent, `<title>$1 `)

	// inject stats
	newContent = strings.ReplaceAll(newContent, `<pre id="footer">`, `
		<script defer data-domain="code.asyncmachine.dev"
			src="https://a.blogic.tech/js/script.outbound-links.js"></script>
		<pre id="footer">`)

	return os.WriteFile(path, []byte(newContent), 0644)
}
