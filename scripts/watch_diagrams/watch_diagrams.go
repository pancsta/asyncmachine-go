package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

var dir = "docs/diagrams"
var target = "assets/asyncmachine-go/diagrams/"

func main() {
	// 1. Create a new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// 2. Start a goroutine to handle events
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// We care about Write (Save) and Create events
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					handleFileChange(event.Name)
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Watcher error:", err)
			}
		}
	}()

	// 3. Add the path to watch (Current directory ".")
	err = watcher.Add(dir)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Watching %s for changes...", dir)

	// 4. Block forever
	<-make(chan struct{})
}

func handleFileChange(filePath string) {
	// Get the file extension
	ext := filepath.Ext(filePath)
	baseName := filepath.Base(filePath)

	switch ext {
	case ".d2":
		// Command: d2 file.d2 file.svg
		layout := "elk"
		if baseName == "tx-lifecycle.d2" {
			layout = "dagre"
		}
		runCmd("d2", "--theme=201", "--layout="+layout, filePath, target+baseName+".dark.svg")

	case ".mermaid", ".mmd":
		// Command: mmdc -i file.mermaid -o file.svg
		runCmd("mmdc", "-i", filePath, "--theme=dark", "-o", target+baseName+".dark.svg", "--backgroundColor=transparent")
		runCmd("mmdc", "-i", filePath, "--theme=forest", "-o", target+baseName+".light.svg", "--backgroundColor=transparent")
	}
}

func runCmd(name string, args ...string) {
	cmd := exec.Command(name, args...)

	// Connect stdout/stderr to the console so you can see compiler errors
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		log.Printf("❌ Error running %s: %v\n", name, err)
	}
}
