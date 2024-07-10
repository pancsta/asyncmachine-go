package debugger

import (
	"regexp"
	"sort"
	"strconv"

	"github.com/pancsta/cview"
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

var humanSortRE = regexp.MustCompile(`[0-9]+`)

func formatTxBarTitle(title string) string {
	return "[::u]" + title + "[::-]"
}

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

		// if no numbers - compare as strings
		posI := humanSortRE.FindStringIndex(strs[i][firstDiff:])
		posJ := humanSortRE.FindStringIndex(strs[j][firstDiff:])
		if len(posI) <= 0 || len(posJ) <= 0 || posI[0] != posJ[0] {
			return strs[i] < strs[j]
		}

		// if contains numbers - sort by numbers
		numsI := humanSortRE.FindAllString(strs[i][firstDiff:], -1)
		numsJ := humanSortRE.FindAllString(strs[j][firstDiff:], -1)
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
			d.matrix.GetCell(row, ii).SetBackgroundColor(colorHighlight)
		}
	}
}
