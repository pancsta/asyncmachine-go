package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

func main() {
	err := extractMermaidDiagrams(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Println("Error:", err)
	}
}

func extractMermaidDiagrams(inputFile string, reqIndexes string) error {
	// Open the Markdown file
	file, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	ri := strings.Split(reqIndexes, " ")

	// Regex to detect mermaid code blocks
	startRegex := regexp.MustCompile("^```mermaid$")
	endRegex := regexp.MustCompile("^```$")

	// Scanner to read the file line by line
	scanner := bufio.NewScanner(file)

	isInMermaidBlock := false
	diagramNumber := 1
	var diagramContent strings.Builder // Store diagram content temporarily

	for scanner.Scan() {
		line := scanner.Text()

		// Check for the start of a mermaid block
		if startRegex.MatchString(line) {
			isInMermaidBlock = true
			diagramContent.Reset() // Clear the previous content
			continue
		}

		// Check for the end of a mermaid block
		if isInMermaidBlock && endRegex.MatchString(line) {
			// skip if not requested
			if !slices.Contains(ri, fmt.Sprintf("%d", diagramNumber)) {
				continue
			}

			isInMermaidBlock = false

			// Write the extracted diagram to a .mmd file
			outputFile := fmt.Sprintf("diagram_%d.mmd", diagramNumber)
			err := os.WriteFile(outputFile, []byte(diagramContent.String()), 0644)
			if err != nil {
				return fmt.Errorf("failed to write diagram file: %v", err)
			}
			fmt.Printf("Extracted diagram %d into %s\n", diagramNumber, outputFile)

			diagramNumber++
			continue
		}

		// Collect lines for the mermaid block
		if isInMermaidBlock {
			diagramContent.WriteString(line + "\n")
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %v", err)
	}

	return nil
}