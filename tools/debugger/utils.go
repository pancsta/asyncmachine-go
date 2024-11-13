package debugger

import (
	"regexp"
	"sort"
	"strconv"
	"unicode"

	"github.com/pancsta/cview"

	"github.com/pancsta/asyncmachine-go/pkg/machine"

	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
)

type Focusable struct {
	cview.Primitive
	*cview.Box
}

type filter struct {
	id     string
	label  string
	active bool
}

// TODO migrate to Provide-Delivered
func RpcGetter(d *Debugger) func(string) any {
	// TODO make it panic-safe (check states and nils)
	return func(name string) any {
		switch name {

		case server.GetCursorTx.Encode():
			return d.C.CursorTx

		case server.GetCursorStep.Encode():
			return d.C.CursorStep

		case server.GetMsgCount.Encode():
			return len(d.C.MsgTxs)

		case server.GetClientCount.Encode():
			return len(d.Clients)

		case server.GetOpts.Encode():
			return d.Opts

		case server.GetSelectedState.Encode():
			return d.C.SelectedState

		}

		return nil
	}
}

func formatTxBarTitle(title string) string {
	return "[::u]" + title + "[::-]"
}

var digitsRe = regexp.MustCompile(`[0-9]+`)

func humanSort(strs []string) {
	sort.Slice(strs, func(i, j int) bool {
		// skip overlapping parts
		maxChars := min(len(strs[i]), len(strs[j]))
		firstDiff := 0
		for k := 0; k < maxChars; k++ {
			if strs[i][k] != strs[j][k] {
				break
			}
			firstDiff++
		}

		// if the diff is a letter, compare as strings
		if !unicode.IsDigit(getRuneAt(strs[i], firstDiff)) ||
			!unicode.IsDigit(getRuneAt(strs[j], firstDiff)) {
			return strs[i] < strs[j]
		}

		// if contains numbers - sort by numbers
		numsI := digitsRe.FindAllString(strs[i][firstDiff:], -1)
		numsJ := digitsRe.FindAllString(strs[j][firstDiff:], -1)
		numI, _ := strconv.Atoi(numsI[0])
		numJ, _ := strconv.Atoi(numsJ[0])

		if numI != numJ {
			// If the numbers are different, order by the numbers
			return numI < numJ
		}

		// If the numbers are the same, order lexicographically
		return strs[i] < strs[j]
	})
}

func getRuneAt(s string, pos int) rune {
	if pos < 0 || pos >= len(s) {
		return 0 // or handle the error as needed
	}
	return []rune(s)[pos]
}

func matrixCellVal(strVal string) string {
	switch len(strVal) {
	case 1:
		strVal = " " + strVal + " "
	case 2:
		strVal = " " + strVal
	}
	return strVal
}

func matrixEmptyRow(d *Debugger, row, colsCount, highlightIndex int) {
	// empty row
	for ii := 0; ii < colsCount; ii++ {
		d.matrix.SetCellSimple(row, ii, "   ")
		if ii == highlightIndex {
			d.matrix.GetCell(row, ii).SetBackgroundColor(colorHighlight3)
		}
	}
}

// findFirstDiff returns the index of the first differing character between two
// strings. If the strings are identical, it returns -1.
func findFirstDiff(s1, s2 string) int {
	minLen := len(s1)
	if len(s2) < minLen {
		minLen = len(s2)
	}

	for i := 0; i < minLen; i++ {
		if s1[i] != s2[i] {
			return i
		}
	}

	if len(s1) != len(s2) {
		return minLen
	}

	return -1
}

func tickStrToTime(mach *machine.Machine, ticksStr []string) machine.Time {
	ticks := make(machine.Time, len(ticksStr))
	for i, t := range ticksStr {
		v, err := strconv.Atoi(t)
		if err != nil {
			mach.AddErr(err, nil)
			continue
		}
		ticks[i] = uint64(v)
	}

	return ticks
}
