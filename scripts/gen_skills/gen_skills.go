package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/pancsta/asyncmachine-go/scripts/shared"
)

func init() {
	shared.GoToRootDir()
}

// ///// ///// /////

// ///// CLI SKILL

// ///// ///// /////

type Subcommand struct {
	Name string
	Help string
}

type Command struct {
	Name        string
	Help        string
	Subcommands []Subcommand
}

type CliData struct {
	Commands map[string]Command
	List     []Command
}

func (d CliData) Cmd(name string) Command {
	return d.Commands[name]
}

func (d CliData) HasCmd(name string) bool {
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

func genSkillCli() error {
	cmdDirs, err := os.ReadDir("tools/cmd")
	if err != nil {
		return fmt.Errorf("failed to read tools/cmd: %w", err)
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
			return fmt.Errorf("failed to run help for %s: %w", cmdName, err)
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

	out, err := renderTemplate("skill_cli.tmpl", CliData{
		Commands: cmdMap,
		List:     commands,
	})
	if err != nil {
		return err
	}

	return writeSkill("am-cli", out)
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

// ///// ///// /////

// ///// AM SKILLS (am, am-cook, am-full, am-mini, am-readme)

// ///// ///// /////

type AmData struct {
	Base         string
	Schema       string
	States       string
	Basic        string
	MachTemplate string
}

type CookData struct {
	Base     string
	Cookbook string
}

type ReadmeSection struct {
	Path    string
	Content string
}

type FullData struct {
	Base   string
	Manual string
}

type ReadmeData struct {
	Base    string
	Readmes []ReadmeSection
}

type MiniData struct {
	Base           string
	Mini           string
	GettingStarted string
}

// genSkillAm generates the base "am" skill out of the general guidelines,
// the example schema, the predefined states, and the basic/mach_template
// examples.
func genSkillAm() (string, error) {
	base, err := readFile("docs/editors/SKILL.md")
	if err != nil {
		return "", err
	}
	schema, err := readFile("docs/editors/schema.go")
	if err != nil {
		return "", err
	}
	// states.md already wraps itself in a ```go fence
	states, err := readFile("docs/editors/states.md")
	if err != nil {
		return "", err
	}
	basic, err := readFile("examples/basic/basic.go")
	if err != nil {
		return "", err
	}
	machTemplate, err := readFile("examples/mach_template/mach_template.go")
	if err != nil {
		return "", err
	}

	out, err := renderTemplate("skill_am.tmpl", AmData{
		Base:         strings.TrimSpace(base),
		Schema:       strings.TrimSpace(schema),
		States:       strings.TrimSpace(states),
		Basic:        strings.TrimSpace(basic),
		MachTemplate: strings.TrimSpace(machTemplate),
	})
	if err != nil {
		return "", err
	}

	return out, writeSkill("am", out)
}

// genSkillCook extends the "am" skill with the cookbook.
func genSkillCook(amSkill string) (string, error) {
	cookbook, err := readMarkdownSection("docs/cookbook.md")
	if err != nil {
		return "", err
	}

	out, err := renderTemplate("skill_cook.tmpl", CookData{
		Base:     renameSkill(amSkill, "am-cook"),
		Cookbook: cookbook,
	})
	if err != nil {
		return "", err
	}

	return out, writeSkill("am-cook", out)
}

// genSkillFull extends the "am-cook" skill with the manual.
func genSkillFull(cookSkill string) error {
	manual, err := readMarkdownSection("docs/manual.md")
	if err != nil {
		return err
	}

	out, err := renderTemplate("skill_full.tmpl", FullData{
		Base:   renameSkill(cookSkill, "am-full"),
		Manual: manual,
	})
	if err != nil {
		return err
	}

	return writeSkill("am-full", out)
}

// genSkillReadme extends the "am" skill with every package README.
func genSkillReadme(amSkill string) error {
	readmes, err := readPackageReadmes()
	if err != nil {
		return err
	}

	out, err := renderTemplate("skill_readme.tmpl", ReadmeData{
		Base:    renameSkill(amSkill, "am-readme"),
		Readmes: readmes,
	})
	if err != nil {
		return err
	}

	return writeSkill("am-readme", out)
}

// genSkillMini extends the general guidelines with the condensed cheatsheet
// and the getting-started tutorial, skipping the longer am-only reference
// material.
func genSkillMini() error {
	base, err := readFile("docs/editors/SKILL.md")
	if err != nil {
		return err
	}
	mini, err := readFile("docs/editors/mini.md")
	if err != nil {
		return err
	}
	gettingStarted, err := readMarkdownSection("docs/getting-started.md")
	if err != nil {
		return err
	}

	out, err := renderTemplate("skill_mini.tmpl", MiniData{
		Base:           renameSkill(strings.TrimSpace(base), "am-mini"),
		Mini:           strings.TrimSpace(mini),
		GettingStarted: gettingStarted,
	})
	if err != nil {
		return err
	}

	return writeSkill("am-mini", out)
}

// readPackageReadmes returns the README.md of every /pkg package, sorted by
// path, for inclusion in the am-readme skill.
func readPackageReadmes() ([]ReadmeSection, error) {
	matches, err := filepath.Glob("pkg/*/README.md")
	if err != nil {
		return nil, fmt.Errorf("failed to glob pkg READMEs: %w", err)
	}
	sort.Strings(matches)

	readmes := make([]ReadmeSection, 0, len(matches))
	for _, path := range matches {
		content, err := readMarkdownSection(path)
		if err != nil {
			return nil, err
		}
		readmes = append(readmes, ReadmeSection{
			Path:    filepath.ToSlash(path),
			Content: content,
		})
	}

	return readmes, nil
}

var frontmatterNameRe = regexp.MustCompile(`(?m)^name: .*$`)

// renameSkill rewrites the `name:` field of a skill's YAML frontmatter.
func renameSkill(content, newName string) string {
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return content
	}
	parts[1] = frontmatterNameRe.ReplaceAllString(parts[1], "name: "+newName)

	return parts[0] + "---" + parts[1] + "---" + parts[2]
}

// stripLeadingH1 removes a leading `# Title` line (and the blank line after
// it) from markdown that's being nested under its own generated heading, to
// avoid two titles stacked on top of each other.
func stripLeadingH1(content string) string {
	lines := strings.SplitN(content, "\n", 2)
	if !strings.HasPrefix(lines[0], "# ") {
		return content
	}
	if len(lines) == 1 {
		return ""
	}

	return strings.TrimLeft(lines[1], "\n")
}

func readMarkdownSection(path string) (string, error) {
	content, err := readFile(path)
	if err != nil {
		return "", err
	}

	return stripLeadingH1(strings.TrimSpace(content)), nil
}

// ///// ///// /////

// ///// SHARED

// ///// ///// /////

func readFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}

	return string(content), nil
}

func renderTemplate(name string, data any) (string, error) {
	path := filepath.Join("scripts", "gen_skills", name)
	tmplContent, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read template %s: %w", name, err)
	}

	tmpl, err := template.New(name).Parse(string(tmplContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}

	return rewriteAssetLinks(b.String()), nil
}

// assetsLinkRe matches markdown links/images and HTML src/srcset attributes
// pointing at a relative "assets/..." path (eg "assets/foo.svg",
// "../assets/foo.svg"), capturing the link opener and the path after
// "assets/".
var assetsLinkRe = regexp.MustCompile(
	`(\[[^\]]*\]\(|(?:src|srcset)=")(?:\.\./|\./)*assets/([^)"]+)`)

// rewriteAssetLinks rewrites relative "assets/..." links into absolute links
// to the publicly hosted assets. Generated skills are copied out of the repo
// (eg into ~/.claude/skills), where the local assets/ symlink doesn't exist,
// so relative links would break.
func rewriteAssetLinks(content string) string {
	return assetsLinkRe.ReplaceAllString(content,
		"${1}https://pancsta.github.io/assets/${2}")
}

// TODO add version
func writeSkill(name, content string) error {
	dir := filepath.Join("docs", "editors", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	file := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}
	fmt.Printf("Generated /%s\n", file)

	return nil
}

func main() {
	amSkill, err := genSkillAm()
	if err != nil {
		panic(fmt.Errorf("gen skill am: %w", err))
	}

	cookSkill, err := genSkillCook(amSkill)
	if err != nil {
		panic(fmt.Errorf("gen skill am-cook: %w", err))
	}

	if err := genSkillFull(cookSkill); err != nil {
		panic(fmt.Errorf("gen skill am-full: %w", err))
	}

	if err := genSkillReadme(amSkill); err != nil {
		panic(fmt.Errorf("gen skill am-readme: %w", err))
	}

	if err := genSkillMini(); err != nil {
		panic(fmt.Errorf("gen skill am-mini: %w", err))
	}

	if err := genSkillCli(); err != nil {
		panic(fmt.Errorf("gen skill am-cli: %w", err))
	}
}
