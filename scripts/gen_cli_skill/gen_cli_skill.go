package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/pancsta/asyncmachine-go/scripts/shared"
)

func init() {
	shared.GoToRootDir()
}

type Subcommand struct {
	Name string
	Help string
}

type Command struct {
	Name        string
	Help        string
	Subcommands []Subcommand
}

type TemplateData struct {
	Commands map[string]Command
	List     []Command
}

func (d TemplateData) Cmd(name string) Command {
	return d.Commands[name]
}

func (d TemplateData) HasCmd(name string) bool {
	_, ok := d.Commands[name]
	return ok
}

func (c Command) Subcmd(name string) Subcommand {
	for _, sub := range c.Subcommands {
		if sub.Name == name {
			return sub
		}
	}
	return Subcommand{}
}

func (c Command) SubcmdHelp(name string) string {
	return c.Subcmd(name).Help
}

func main() {
	cmdDirs, err := os.ReadDir("tools/cmd")
	if err != nil {
		panic(fmt.Errorf("failed to read tools/cmd: %w", err))
	}

	var commands []Command
	cmdMap := make(map[string]Command)

	for _, entry := range cmdDirs {
		if !entry.IsDir() {
			continue
		}
		cmdName := entry.Name()
		cmdPkg := fmt.Sprintf("./tools/cmd/%s", cmdName)

		rootHelp, err := runHelp(cmdPkg)
		if err != nil {
			panic(fmt.Errorf("failed to run help for %s: %w", cmdName, err))
		}
		rootHelp = strings.TrimSpace(rootHelp)

		subcmdNames := extractSubcommands(rootHelp)
		var subcommands []Subcommand
		for _, sub := range subcmdNames {
			if sub == "help" || sub == "completion" || sub == "states-file" {
				continue
			}
			subHelp, err := runHelp(cmdPkg, sub)
			if err != nil {
				// Some subcommands might not support help; continue
				continue
			}
			subcommands = append(subcommands, Subcommand{
				Name: sub,
				Help: strings.TrimSpace(subHelp),
			})
		}

		cmd := Command{
			Name:        cmdName,
			Help:        rootHelp,
			Subcommands: subcommands,
		}
		commands = append(commands, cmd)
		cmdMap[cmdName] = cmd
	}

	tmplContent, err := os.ReadFile(filepath.Join("scripts", "gen_cli_skill", "skill.tmpl"))
	if err != nil {
		panic(fmt.Errorf("failed to read template: %w", err))
	}

	tmpl, err := template.New("skill").Parse(string(tmplContent))
	if err != nil {
		panic(fmt.Errorf("failed to parse template: %w", err))
	}

	var b bytes.Buffer
	data := TemplateData{
		Commands: cmdMap,
		List:     commands,
	}

	if err := tmpl.Execute(&b, data); err != nil {
		panic(fmt.Errorf("failed to execute template: %w", err))
	}

	targetDir := filepath.Join("docs", "editors", "skills", "am-cli")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		panic(fmt.Errorf("failed to create target dir %s: %w", targetDir, err))
	}

	targetFile := filepath.Join(targetDir, "SKILL.md")
	if err := os.WriteFile(targetFile, b.Bytes(), 0644); err != nil {
		panic(fmt.Errorf("failed to write %s: %w", targetFile, err))
	}

	fmt.Printf("Generated /%s\n", targetFile)
}

func runHelp(args ...string) (string, error) {
	cmdArgs := append([]string{"run"}, args...)
	cmdArgs = append(cmdArgs, "--help")

	cmd := exec.Command("go", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	if out == "" {
		out = stderr.String()
	}
	return out, err
}

func extractSubcommands(helpText string) []string {
	var subcmds []string
	seen := make(map[string]bool)

	// 1. go-arg Commands: section
	inCommandsSection := false
	for _, line := range strings.Split(helpText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "Commands:") {
			inCommandsSection = true
			continue
		}
		if inCommandsSection {
			if trimmed == "" {
				inCommandsSection = false
				continue
			}
			if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
				fields := strings.Fields(trimmed)
				if len(fields) > 0 {
					name := fields[0]
					if !seen[name] {
						seen[name] = true
						subcmds = append(subcmds, name)
					}
				}
			} else {
				inCommandsSection = false
			}
		}
	}

	// 2. Cobra sections (e.g. Mutations, Waiting, Checking, REPL, Additional Commands)
	cobraHeaderRe := regexp.MustCompile(`^(Mutations|Waiting|Checking|REPL|Available Commands|Commands):\s*$`)
	inCobraSection := false
	for _, line := range strings.Split(helpText, "\n") {
		trimmed := strings.TrimSpace(line)
		if cobraHeaderRe.MatchString(trimmed) {
			inCobraSection = true
			continue
		}
		if inCobraSection {
			if trimmed == "" {
				inCobraSection = false
				continue
			}
			if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
				fields := strings.Fields(trimmed)
				if len(fields) > 0 {
					name := fields[0]
					if !seen[name] && !strings.HasPrefix(name, "-") {
						seen[name] = true
						subcmds = append(subcmds, name)
					}
				}
			} else {
				inCobraSection = false
			}
		}
	}

	return subcmds
}
