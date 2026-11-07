package generator

// am-gen lint refactors an existing ss_*.go file: it orders the entries of an
// am.Schema literal according to the field order of the matching
// {Name}StatesDef struct and appends empty entries for states which are
// declared in the struct, but missing in the schema.
//
// The refactor works on the original source bytes (go/ast for positions only),
// so formatting and comments are preserved. The result is passed through
// go/format as a safety net.

import (
	"bytes"
	"fmt"
	"go/ast"
	goformat "go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// LintResult is the outcome of [LintSchemaOrder].
type LintResult struct {
	// Src is the linted source (equal to the input when Changed is false).
	Src []byte
	// Changed is true when Src differs from the input.
	Changed bool
	// Added lists the states which were added to the schema.
	Added []string
	// Reordered is true when existing schema entries changed their order.
	Reordered bool
}

// schemaLit is an am.Schema composite literal along with the name of the var
// it belongs to.
type schemaLit struct {
	lit     *ast.CompositeLit
	varName string
}

// lintEdit is a byte-range replacement within the linted source.
type lintEdit struct {
	start, end int
	repl       string
}

// LintSchemaOrder orders the entries of every am.Schema literal in src
// according to the field order of the matching {Name}StatesDef struct and adds
// entries for missing states. The result is gofmt-ed.
func LintSchemaOrder(filename string, src []byte) (*LintResult, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	defs := collectStatesDefs(file)
	prefixDefs := collectStatesPrefixes(file, defs)
	lits := collectSchemaLits(file)

	var edits []lintEdit
	var added []string
	reordered := false

	for _, sl := range lits {
		defName, fields := resolveStatesDef(sl, defs, prefixDefs)
		if defName == "" || len(fields) == 0 {
			// no matching StatesDef (e.g. a bare schema without a struct)
			continue
		}

		edit, add, reord, ok, err := lintSchemaLit(
			fset, src, sl, defName, fields, prefixDefs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filename, err)
		}
		if !ok {
			continue
		}

		edits = append(edits, edit)
		added = append(added, add...)
		reordered = reordered || reord
	}

	if len(edits) == 0 {
		return &LintResult{Src: src}, nil
	}

	// apply from the end, so earlier offsets stay valid
	sort.Slice(edits, func(i, j int) bool {
		return edits[i].start > edits[j].start
	})
	out := src
	for _, e := range edits {
		out = spliceBytes(out, e.start, e.end, e.repl)
	}

	formatted, err := goformat.Source(out)
	if err != nil {
		return nil, fmt.Errorf("format %s: %w", filename, err)
	}

	res := &LintResult{
		Src:       formatted,
		Added:     added,
		Reordered: reordered,
	}
	res.Changed = !bytes.Equal(formatted, src)
	if !res.Changed {
		res.Src = src
		res.Added = nil
		res.Reordered = false
	}

	return res, nil
}

// lintSchemaLit lints a single am.Schema literal. ok is false when the literal
// cannot be processed (e.g. unexpected keys) and should be left untouched.
func lintSchemaLit(
	fset *token.FileSet, src []byte, sl schemaLit, defName string,
	fields []string, prefixDefs map[string]string,
) (edit lintEdit, added []string, reordered, ok bool, err error) {
	lit := sl.lit
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	prefix := litPrefix(lit)
	if prefix == "" {
		prefix = fallbackStatesPrefix(defName, prefixDefs)
	}

	bodyStart := offset(lit.Lbrace) + 1
	bodyEnd := offset(lit.Rbrace)

	// no entries yet: generate all of them
	if len(lit.Elts) == 0 {
		baseIndent := lineIndent(src, offset(lit.Lbrace))
		indent := baseIndent + "\t"
		var parts []string
		for _, f := range fields {
			parts = append(parts, indent+prefix+"."+f+": {},")
			added = append(added, f)
		}
		if len(parts) == 0 {
			return edit, nil, false, false, nil
		}
		body := "\n" + strings.Join(parts, "\n") + "\n" + baseIndent
		return lintEdit{start: bodyStart, end: bodyEnd, repl: body},
			added, false, true, nil
	}

	// single line (or a closing brace sharing the line with the last entry):
	// rebuild compactly, comments inside such a literal are not supported
	lastEndLine := fset.Position(lit.Elts[len(lit.Elts)-1].End()).Line
	if fset.Position(lit.Lbrace).Line == fset.Position(lit.Rbrace).Line ||
		lastEndLine == fset.Position(lit.Rbrace).Line {
		return lintSchemaLitCompact(fset, src, sl, fields, prefix)
	}

	type block struct {
		state       string
		text        string
		order       int
		blankBefore bool
	}
	stateOrder := func(name string) int {
		for i, f := range fields {
			if f == name {
				return i
			}
		}
		return -1
	}

	var blocks []block
	// gaps[i] is the text before block i, gaps[len(blocks)] is the text
	// between the last block and the closing brace
	var gaps []string
	lastEnd := bodyStart

	for i, elt := range lit.Elts {
		kv, isKV := elt.(*ast.KeyValueExpr)
		if !isKV {
			return edit, nil, false, false, nil
		}
		sel, isSel := kv.Key.(*ast.SelectorExpr)
		if !isSel {
			return edit, nil, false, false, nil
		}
		state := sel.Sel

		start := attachCommentStart(src, offset(kv.Pos()))
		end := lineEndOffset(src, offset(kv.End()))
		if end > bodyEnd {
			end = bodyEnd
		}
		var gap string
		if i == 0 {
			gap = string(src[bodyStart:start])
		} else {
			gap = string(src[lastEnd:start])
		}
		gaps = append(gaps, gap)
		blocks = append(blocks, block{
			state:       state.Name,
			text:        string(src[start:end]),
			order:       stateOrder(state.Name),
			blankBefore: strings.Contains(gap, "\n\n"),
		})
		lastEnd = end
	}
	gaps = append(gaps, string(src[lastEnd:bodyEnd]))

	// add missing states
	existing := make(map[string]bool, len(blocks))
	for _, blk := range blocks {
		existing[blk.state] = true
	}
	indent := leadingWhitespace(blocks[0].text)
	if indent == "" {
		indent = lineIndent(src, offset(lit.Lbrace)) + "\t"
	}
	for _, f := range fields {
		if existing[f] {
			continue
		}
		blocks = append(blocks, block{
			state: f,
			text:  indent + prefix + "." + f + ": {},",
			order: stateOrder(f),
		})
		added = append(added, f)
	}

	// stable sort: unknown states (e.g. overridden inherited ones) float to
	// the top in their original order
	sorted := make([]block, len(blocks))
	copy(sorted, blocks)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].order < sorted[j].order
	})
	for i := range sorted {
		if sorted[i].state != blocks[i].state {
			reordered = true
			break
		}
	}

	// blank lines travel with their entries, comment paragraphs always get one
	prefixGap := gaps[0]
	suffixGap := gaps[len(gaps)-1]
	var body strings.Builder
	for i, blk := range sorted {
		gap := "\n"
		if blk.blankBefore ||
			strings.HasPrefix(strings.TrimLeft(blk.text, " \t"), "//") {
			gap = "\n\n"
		}
		if i == 0 {
			gap = prefixGap
		}
		body.WriteString(gap)
		body.WriteString(blk.text)
	}
	body.WriteString(suffixGap)

	return lintEdit{start: bodyStart, end: bodyEnd, repl: body.String()},
		added, reordered, true, nil
}

// lintSchemaLitCompact rebuilds a single-line schema literal.
func lintSchemaLitCompact(
	fset *token.FileSet, src []byte, sl schemaLit, fields []string,
	prefix string,
) (edit lintEdit, addedStates []string, reordered, ok bool, err error) {
	lit := sl.lit
	offset := func(p token.Pos) int { return fset.Position(p).Offset }

	type entry struct {
		state string
		key   string
		value string
		order int
	}
	stateOrder := func(name string) int {
		for i, f := range fields {
			if f == name {
				return i
			}
		}
		return -1
	}

	var entries []entry
	for _, elt := range lit.Elts {
		kv, isKV := elt.(*ast.KeyValueExpr)
		if !isKV {
			return edit, nil, false, false, nil
		}
		sel, isSel := kv.Key.(*ast.SelectorExpr)
		if !isSel {
			return edit, nil, false, false, nil
		}
		state := sel.Sel
		entries = append(entries, entry{
			state: state.Name,
			key:   string(src[offset(kv.Key.Pos()):offset(kv.Key.End())]),
			value: string(src[offset(kv.Value.Pos()):offset(kv.Value.End())]),
			order: stateOrder(state.Name),
		})
	}

	existing := make(map[string]bool, len(entries))
	for _, e := range entries {
		existing[e.state] = true
	}
	for _, f := range fields {
		if existing[f] {
			continue
		}
		entries = append(entries, entry{
			state: f,
			key:   prefix + "." + f,
			value: "{}",
			order: stateOrder(f),
		})
		addedStates = append(addedStates, f)
	}

	sorted := make([]entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].order < sorted[j].order
	})
	for i := range sorted {
		if sorted[i].state != entries[i].state {
			reordered = true
			break
		}
	}

	var parts []string
	for _, e := range sorted {
		parts = append(parts, e.key+": "+e.value)
	}
	// the replacement covers the inside of the braces only
	body := strings.Join(parts, ", ")

	return lintEdit{
			start: offset(lit.Lbrace) + 1,
			end:   offset(lit.Rbrace),
			repl:  body,
		},
		addedStates, reordered, true, nil
}

// ///// ///// /////

// ///// AST

// ///// ///// /////

// collectStatesDefs returns the ordered string fields of all {Name}StatesDef
// structs in the file, indexed by the struct name.
func collectStatesDefs(file *ast.File) map[string][]string {
	res := make(map[string][]string)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(ts.Name.Name, "StatesDef") {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			var fields []string
			for _, f := range st.Fields.List {
				// skip embedded structs
				if len(f.Names) == 0 {
					continue
				}
				if id, ok := f.Type.(*ast.Ident); !ok || id.Name != "string" {
					continue
				}
				for _, n := range f.Names {
					fields = append(fields, n.Name)
				}
			}
			res[ts.Name.Name] = fields
		}
	}
	return res
}

// collectStatesPrefixes maps states var names (eg ssM) to StatesDef names, by
// looking for {Name}StatesDef{} passed to am.NewStates.
func collectStatesPrefixes(
	file *ast.File, defs map[string][]string,
) map[string]string {
	res := make(map[string]string)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			call, ok := vs.Values[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "NewStates" || len(call.Args) != 1 {
				continue
			}
			defName := compositeTypeName(call.Args[0])
			if _, ok := defs[defName]; !ok {
				continue
			}
			res[vs.Names[0].Name] = defName
		}
	}
	return res
}

// collectSchemaLits returns all am.Schema literals which are values of a var,
// skipping the receiver of am.Schema{}.Merge(...).
func collectSchemaLits(file *ast.File) []schemaLit {
	varNames := make(map[*ast.CompositeLit]string)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			name := ""
			if len(vs.Names) > 0 {
				name = vs.Names[0].Name
			}
			for _, v := range vs.Values {
				ast.Inspect(v, func(n ast.Node) bool {
					if lit, ok := n.(*ast.CompositeLit); ok && isAmSchemaLit(lit) {
						varNames[lit] = name
					}
					return true
				})
			}
		}
	}

	var res []schemaLit
	astutil.Apply(file, func(c *astutil.Cursor) bool {
		lit, ok := c.Node().(*ast.CompositeLit)
		if !ok || !isAmSchemaLit(lit) {
			return true
		}
		// skip the am.Schema{}.Merge(...) receiver
		if sel, ok := c.Parent().(*ast.SelectorExpr); ok && sel.X == lit {
			return true
		}
		name, ok := varNames[lit]
		if !ok {
			return true
		}
		res = append(res, schemaLit{lit: lit, varName: name})
		return true
	}, nil)

	return res
}

// resolveStatesDef finds the {Name}StatesDef matching a schema literal.
func resolveStatesDef(
	sl schemaLit, defs map[string][]string, prefixDefs map[string]string,
) (string, []string) {
	defName := ""
	// by the states var prefix (eg ssM)
	if p := litPrefix(sl.lit); p != "" {
		defName = prefixDefs[p]
	}
	// by the schema var name (eg MyMachSchema -> MyMachStatesDef)
	if defName == "" && strings.HasSuffix(sl.varName, "Schema") {
		candidate := strings.TrimSuffix(sl.varName, "Schema") + "StatesDef"
		if _, ok := defs[candidate]; ok {
			defName = candidate
		}
	}
	// fall back to the only StatesDef in the file
	if defName == "" && len(defs) == 1 {
		for name := range defs {
			defName = name
		}
	}
	if defName == "" {
		return "", nil
	}
	return defName, defs[defName]
}

// litPrefix returns the package-level var prefix of schema entries (eg ssM in
// ssM.Start).
func litPrefix(lit *ast.CompositeLit) string {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		sel, ok := kv.Key.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		return id.Name
	}
	return ""
}

// fallbackStatesPrefix derives the states var name for an empty schema literal.
func fallbackStatesPrefix(defName string, prefixDefs map[string]string) string {
	for prefix, def := range prefixDefs {
		if def == defName {
			return prefix
		}
	}
	base := strings.TrimSuffix(defName, "StatesDef")
	if base == "" {
		return "ss"
	}
	return "ss" + strings.ToUpper(base[:1])
}

func isAmSchemaLit(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "am" && sel.Sel.Name == "Schema"
}

func compositeTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.CompositeLit:
		if id, ok := t.Type.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.UnaryExpr:
		return compositeTypeName(t.X)
	case *ast.ParenExpr:
		return compositeTypeName(t.X)
	}
	return ""
}

// ///// ///// /////

// ///// SOURCE

// ///// ///// /////

// attachCommentStart walks up from the entry line over blank lines and
// contiguous // comments, returning the start of the attached comment
// paragraph (or the entry line when there is none).
func attachCommentStart(src []byte, entryOff int) int {
	start := lineStartOffset(src, entryOff)
	pos := start
	for pos > 0 {
		prevEnd := pos - 1
		prevStart := lineStartOffset(src, prevEnd)
		line := strings.TrimSpace(string(src[prevStart:prevEnd]))
		if line == "" {
			pos = prevStart
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
		start = prevStart
		pos = prevStart
	}
	return start
}

func lineStartOffset(src []byte, off int) int {
	if off > len(src) {
		off = len(src)
	}
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}

func lineEndOffset(src []byte, off int) int {
	if off > len(src) {
		off = len(src)
	}
	for off < len(src) && src[off] != '\n' {
		off++
	}
	return off
}

// lineIndent returns the leading whitespace of the line containing off.
func lineIndent(src []byte, off int) string {
	start := lineStartOffset(src, off)
	end := start
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return string(src[start:end])
}

func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

func spliceBytes(src []byte, start, end int, repl string) []byte {
	buf := make([]byte, 0, len(src)-(end-start)+len(repl))
	buf = append(buf, src[:start]...)
	buf = append(buf, repl...)
	buf = append(buf, src[end:]...)
	return buf
}
