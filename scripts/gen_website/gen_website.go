// script/website/main.go
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	md2html "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/joho/godotenv"

	"github.com/pancsta/asyncmachine-go/scripts/gen_website/sitemap"
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}
}

var apiUrl = os.Getenv("AM_DEPLOY_API_URL")

var amMainMenu = sitemap.MainMenu

const infoIcon = `<svg class=align-bottom xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6" style="width: 25px;display: inline;">
  <path stroke-linecap="round" stroke-linejoin="round" d="m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z"></path>
</svg>`

func main() {
	outputDir := filepath.Join("docs", "website")

	// 1. Create the output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		panic(fmt.Errorf("failed to create output directory: %w", err))
	}

	fmt.Printf("Rendering README.md files...\n")
	os.MkdirAll(outputDir, 0755)

	for _, e := range amMainMenu {
		if e.Path == "" {
			continue
		}
		if err := renderFile(e, outputDir); err != nil {
			fmt.Printf("Error rendering %s: %v\n", e.Path, err)
		} else {
			// fmt.Printf("Rendered: %s\n", e.Path)
		}
	}

	fmt.Println("Done.")
}

func renderFile(e sitemap.Entry, outputDir string) error {
	// A. Read the Markdown file
	sourcePath := e.Path
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}

	// B. Normalize newlines
	content = markdown.NormalizeNewlines(content)

	// C. Configure Parser
	// Enable common GitHub-like extensions (tables, strikethrough, autolinks)
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	p.Opts.ParserHook = ParserHook

	// Parse content into AST
	doc := p.Parse(content)

	// TODO optimize with classes html.New(html.WithClasses(true))

	// D. Configure Renderer
	// We purposely do NOT add a full page skeleton.
	// Default behavior renders an HTML fragment.
	htmlFlags := md2html.CommonFlags | md2html.HrefTargetBlank
	opts := md2html.RendererOptions{
		Flags:          htmlFlags,
		RenderNodeHook: RenderHook,
	}
	renderer := md2html.NewRenderer(opts)

	// Render AST to HTML
	htmlContent := markdown.Render(doc, renderer)

	// E. Generate Flat Filename
	flatName := e.Url + ".html"
	if sourcePath == "README.md" {
		flatName = "index.html"
	}

	outputPath := filepath.Join(outputDir, flatName)

	// fix ULs
	ret := strings.ReplaceAll(string(htmlContent), "<ul>",
		`<ul class="ps-5 list-disc list-outside">`)

	// fix <code> color
	ret = strings.ReplaceAll(ret, "<code>", `<code class="dark:bg-slate-700 p-1 rounded-sm">`)

	// wrap in layout
	layout, err := os.ReadFile("docs/website/templates/layout.html")
	if err != nil {
		return err
	}
	ret = strings.ReplaceAll(string(layout), "{{ CONTENT }}", ret)
	ret = strings.ReplaceAll(ret, "{{ NAVIGATION }}", renderMainMenu(sourcePath))
	ret, err = processHtml(e, ret)
	if err != nil {
		return err
	}

	// F. Write to file
	return os.WriteFile(outputPath, []byte(ret), 0644)
}

func processHtml(e sitemap.Entry, htmlContent string) (string, error) {
	sourcePath := e.Path
	// 2. Load the HTML into goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		log.Fatal(err)
	}

	// 3. Configure Chroma
	// We select the "go" lexer, "monokai" style, and HTML formatter
	lexerGo := lexers.Get("go")
	if lexerGo == nil {
		lexerGo = lexers.Fallback
	}
	lexerGo = chroma.Coalesce(lexerGo)
	lexerBash := lexers.Get("bash")
	if lexerBash == nil {
		lexerBash = lexers.Fallback
	}
	lexerBash = chroma.Coalesce(lexerBash)
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	// WithClasses(false) puts CSS styles inline (style="color:...")
	// If you want to use a separate CSS file, set this to true.
	formatter := html.New(html.WithClasses(false), html.TabWidth(4))

	// highlight code
	doc.Find("pre > code.language-go").Each(func(i int, s *goquery.Selection) {
		highlightCode(s, lexerGo, formatter, style)
	})
	doc.Find("pre > code.language-bash").Each(func(i int, s *goquery.Selection) {
		highlightCode(s, lexerBash, formatter, style)
	})
	doc.Find("pre > code").Parent().AddClass("p-2 rounded-lg overflow-x-auto")

	// fix ULs
	doc.Find("#page-content > ul").RemoveClass("ps-5")

	// fix readme prefix
	if !strings.Contains(sourcePath, "/") {
		// main readme
		doc.Find("#page-content > h1").PrevAll().Remove().End().Remove()
		title := "/" + e.Path
		if sourcePath == "README.md" {
			title = "/"
		}
		doc.Find("h1").SetText(title)
	} else {
		// nested readmes
		doc.Find("#page-content > blockquote").PrevAll().Remove().End().Remove()
		doc.Find("h1").SetText("/" + e.Path)
	}

	// fix readme suffix
	doc.Find("#monorepo").NextAll().Remove().End().Remove()

	// TODO rewrite links
	//  - .md files to slugs (find missing slugs)
	//  - dir links with README.md to slugs
	//  - other links to code.am.dev
	doc.Find("#page-content a[href]").Each(func(i int, s *goquery.Selection) {
		href := s.AttrOr("href", "")

		// skip external and local
		if strings.HasPrefix(href, "http") || strings.HasPrefix(href, "#") {
			return
		}

		for _, e := range amMainMenu {
			if e.Path == "" {
				continue
			}

			// to github
			if strings.HasSuffix(href, "_test.go") && strings.HasPrefix(href, "/") {
				s.SetAttr("href", fmt.Sprintf("https://github.com/pancsta/asyncmachine-go/blob/main%s", href))
				// fmt.Printf("github link %s\n", href)
				return
			}

			// to code
			if (strings.HasSuffix(href, ".go") || strings.HasSuffix(href, ".json")) &&
				strings.HasPrefix(href, "/") {

				s.SetAttr("href", fmt.Sprintf("%s/src/github.com/pancsta/asyncmachine-go%s.html", apiUrl, href))
				// fmt.Printf("code link %s\n", href)
				return
			}

			// to slugs
			href2 := strings.TrimPrefix(href, "/")
			path2 := strings.TrimSuffix(e.Path, "README.md")
			if strings.HasPrefix(e.Path, href2) || (strings.HasPrefix(href2, path2) && path2 != "") {
				newHref := "/" + e.Url
				if strings.Contains(href2, "#") {
					newHref += href2[strings.Index(href2, "#"):]
				}
				s.SetAttr("href", newHref)
				// fmt.Printf("link %s -> %s\n", href, newHref)
				return
			}
		}

		// some dirs to code
		if !strings.Contains(href, ".") && (strings.HasPrefix(href, "/tools") || strings.HasPrefix(href, "/pkg")) {
			s.SetAttr("href", fmt.Sprintf("%s/pkg/github.com/pancsta/asyncmachine-go%s.html", apiUrl, href))
			// fmt.Printf("code dir link %s\n", href)
			return
		}

		// log err, fallback to github
		fmt.Printf("unhandled link %s\n", href)
		s.SetAttr("href", "https://github.com/pancsta/asyncmachine-go/tree/main"+href)
	})
	doc.Find("#footer-packages a").Each(func(i int, s *goquery.Selection) {
		s.SetAttr("href", fmt.Sprintf(
			"%s/pkg/github.com/pancsta/asyncmachine-go/pkg/%s.html", apiUrl, s.Text(),
		))
	})
	doc.Find("#footer-tools a").Each(func(i int, s *goquery.Selection) {
		s.SetAttr("href", fmt.Sprintf(
			"%s/pkg/github.com/pancsta/asyncmachine-go/tools/%s.html", apiUrl, s.Text(),
		))
	})
	doc.Find("header a:contains(APIs)").SetAttr("href", apiUrl)

	// parse github alerts
	doc.Find(`blockquote:contains("[!NOTE]")`).Each(func(i int, s *goquery.Selection) {
		s.Prev().AddClass("mb-1")
		text := strings.ReplaceAll(strings.TrimSpace(s.Text()), "[!NOTE]\n", "")

		alert := `
		<blockquote class="border-l-3 border-blue-500 pl-3 pb-2">
			<p class="text-blue-500 mb-1 py-1">
				` + infoIcon + ` Note
			</p>
			<p>` + text + `</p>
		</blockquote>`

		s.ReplaceWithHtml(alert)
	})

	// 6. output the modified HTML
	// We use Find("html") to get the whole document string including <html> tags
	result, err := doc.Find("html").Html()
	if err != nil {
		log.Fatal(err)
	}

	return result, nil
}

func highlightCode(s *goquery.Selection, lexerBash chroma.Lexer, formatter *html.Formatter, style *chroma.Style) {
	// Get the raw source code text inside the <code> block
	sourceCode := s.Text()

	// Highlight the code using Chroma
	iterator, err := lexerBash.Tokenise(nil, sourceCode)
	if err != nil {
		log.Printf("Tokenization error: %v", err)
		return
	}

	var buf bytes.Buffer
	err = formatter.Format(&buf, style, iterator)
	if err != nil {
		log.Printf("Formatting error: %v", err)
		return
	}
	highlightedHTML := buf.String()

	// 5. Replace the original block.
	// Since Chroma generates its own <pre> wrapper, we usually want to
	// replace the parent <pre> of our <code> selection to avoid <pre><pre>...</pre></pre>
	s.Parent().ReplaceWithHtml(highlightedHTML)
}

func renderMainMenu(sourcePath string) string {
	selected := "flex-shrink-0 px-4 py-2 text-sm font-semibold text-white bg-blue-600 dark:text-black dark:bg-sunlit-clay-400 rounded-full shadow-md"
	normal := "flex-shrink-0 px-4 py-2 text-sm font-medium text-gray-400 hover:text-white hover:bg-gray-700/80 dark:hover:bg-sunlit-clay-700/80 rounded-full transition-all duration-200"

	ret := ""
	for _, e := range amMainMenu {
		if e.SkipMenu {
			continue
		}
		// separator
		if e.Path == "" {
			ret += `<span class="p-1">•</span>`
			ret += "\n"
			continue
		}
		class := normal
		if e.Path == sourcePath {
			ret += fmt.Sprintf(`
			<script>
            const pageSlug = "%s";
			</script>`, e.Url)
			class = selected
		}
		name := e.Url
		if e.Url == "" {
			name = "/"
		}
		ret += fmt.Sprintf(`<a href="/%s" class="%s" id=nav-%s>%s</a>`, e.Url, class, e.Url, name)
		ret += "\n"
	}

	return mainMenuPre + ret + mainMenuPost
}

const mainMenuPre = `
    <div class="w-full flex justify-center pt-6 px-4 pointer-events-none">
        <nav class="pointer-events-auto bg-gray-800/90 backdrop-blur-md border border-gray-700/50 p-1.5 rounded-full shadow-2xl overflow-x-auto max-w-full no-scrollbar">
            <div class="flex space-x-1">
`

const mainMenuPost = `
            </div>
        </nav>
    </div>
`

// <details> fix
// TODO https://github.com/gomarkdown/markdown/issues/276

func ParserHook(data []byte) (ast.Node, []byte, int) {
	if node, d, n := parseDetails(data); node != nil {
		return node, d, n
	}
	return nil, nil, 0
}

type Details struct {
	ast.Container
}

const (
	detailsBegin = "<details>"
	detailsEnd   = "</details>"
)

func parseDetails(data []byte) (ast.Node, []byte, int) {
	if !bytes.HasPrefix(data, []byte(detailsBegin)) {
		return nil, nil, 0
	}
	start := len(detailsBegin)
	end := bytes.Index(data[start:], []byte(detailsEnd)) + start
	if end < 0 {
		return nil, nil, 0
	}
	return &Details{}, data[start:end], end + len(detailsEnd)
}

func RenderHook(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	switch n := node.(type) {
	case *Details:
		renderDetails(w, n, entering)
		return ast.GoToNext, true
	}
	return ast.GoToNext, false
}

func renderDetails(w io.Writer, details *Details, entering bool) {
	if entering {
		io.WriteString(w, detailsBegin)
	} else {
		io.WriteString(w, detailsEnd)
	}
}
