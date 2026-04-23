package debugger

import (
	"errors"
	"fmt"

	"github.com/pancsta/cview"
	"github.com/pancsta/tcell-v2"
)

// ///// ///// /////

// ///// THEMES

// ///// ///// /////

var themeDark = map[string]string{
	"active":     "yellow",
	"active2":    "greenyellow",
	"inactive":   "limegreen",
	"highlight":  "darkslategrey",
	"highlight2": "dimgrey",
	"highlight3": tcell.Color233.CSS(),
	"err":        "red",
	"errBg":      "indianred",
	"errRecent":  "#FF5FAF",
	"grey":       "darkgrey",
	"white":      "white",
	"yellow":     "yellow",
	"darkGrey":   "darkgrey",
	"green":      "green",
	"lightGrey":  "darkgrey",

	// defaults

	"title":    tcell.ColorWhite.CSS(),
	"border":   tcell.ColorGrey.CSS(),
	"graphics": tcell.ColorWhite.CSS(),

	"fgPrimary":         tcell.ColorWhite.CSS(),
	"fgSecondary":       tcell.ColorYellow.CSS(),
	"tertiary":          tcell.ColorLimeGreen.CSS(),
	"inverse":           tcell.ColorBlack.CSS(),
	"contrastPrimary":   tcell.ColorBlack.CSS(),
	"contrastSecondary": tcell.ColorLightSlateGray.CSS(),

	"bgPrimary":      tcell.ColorBlack.CSS(),
	"bgContrast":     tcell.ColorGreen.CSS(),
	"bgMoreContrast": tcell.ColorDarkGreen.CSS(),

	"scrollBar": tcell.ColorWhite.CSS(),
}

// TODO fix scollbars, log colors
var themeLight = map[string]string{
	"active":            "#7B6200",
	"active2":           "#005500",
	"inactive":          "#007700",
	"highlight":         "#C8DCE0",
	"highlight2":        "#B0B8BB",
	"highlight3":        "#EBEBEB",
	"err":               "#CC0000",
	"errBg":             "#B03030",
	"errRecent":         "#C7006B",
	"grey":              "#555555",
	"white":             "#111111",
	"yellow":            "#8B7500",
	"darkGrey":          "#888888",
	"green":             "#006400",
	"bgPrimary":         tcell.ColorWhite.CSS(),
	"bgContrast":        tcell.ColorOldLace.CSS(),
	"bgMoreContrast":    tcell.ColorLightGrey.CSS(),
	"fgPrimary":         tcell.ColorBlack.CSS(),
	"fgSecondary":       tcell.ColorDimGrey.CSS(),
	"border":            tcell.ColorGrey.CSS(),
	"title":             tcell.ColorBlack.CSS(),
	"graphics":          tcell.ColorBlack.CSS(),
	"tertiary":          tcell.ColorDarkGreen.CSS(),
	"inverse":           tcell.ColorWhite.CSS(),
	"contrastPrimary":   tcell.ColorWhite.CSS(),
	"contrastSecondary": tcell.ColorDarkGrey.CSS(),
	"scrollBar":         tcell.ColorBlack.CSS(),

	// TODO
	"lightGrey": "darkgrey",
}

// TODO move to types
type Theme struct {
	// primary active/selected state color
	Active string
	// secondary active color (changed states)
	Active2 string
	// inactive/default state color
	Inactive string
	// background highlight (selected items)
	Highlight string
	// secondary highlight (scrollbars, selections)
	Highlight2 string
	// tertiary highlight (table/matrix cells)
	Highlight3 string
	// error color
	Err string
	// error background color (accepted tx with exception state)
	ErrBg string
	// recent error indicator
	ErrRecent string
	// inline tview markup grey text
	Grey string
	// inline tview markup white/bright text
	White string
	// log prefix brackets
	Yellow string
	// faded/extern content
	DarkGrey string
	// accepted gutter, relation start
	Green string
	// addr button background
	LightGrey string

	// style overrides (cview.Styles)
	// title text color
	Title string
	// border color
	Border string
	// graphics color
	Graphics string

	// primitive background color
	BgPrimary string
	// contrast background color
	BgContrast string
	// more contrast background color
	BgMoreContrast string

	// primary text color
	FgPrimary string
	// secondary text color
	FgSecondary string
	// tertiary text color
	Tertiary string
	// inverse text color
	Inverse string
	// contrast primary text color
	ContrastPrimary string
	// contrast secondary text color
	ContrastSecondary string

	// scroll bar color
	ScrollBar string
}

// mapToTheme converts a string map into a Theme struct.
func mapToTheme(theme map[string]string, isDark bool) (Theme, error) {
	var errs error
	// AI add safety fallback
	get := func(name string) string {
		v, ok := theme[name]
		if ok {
			return v
		}

		errs = errors.Join(errs, fmt.Errorf("missing theme key: %s", name))
		// fallback TODO log err
		if isDark {
			return "white"
		}
		return "black"
	}

	return Theme{
		Active:     get("active"),
		Active2:    get("active2"),
		Inactive:   get("inactive"),
		Highlight:  get("highlight"),
		Highlight2: get("highlight2"),
		Highlight3: get("highlight3"),
		Err:        get("err"),
		ErrBg:      get("errBg"),
		ErrRecent:  get("errRecent"),
		Grey:       get("grey"),
		White:      get("white"),
		Yellow:     get("yellow"),
		DarkGrey:   get("darkGrey"),
		Green:      get("green"),
		LightGrey:  get("lightGrey"),

		// defaults

		Title:             get("title"),
		Border:            get("border"),
		Graphics:          get("graphics"),
		BgPrimary:         get("bgPrimary"),
		BgContrast:        get("bgContrast"),
		BgMoreContrast:    get("bgMoreContrast"),
		FgPrimary:         get("fgPrimary"),
		FgSecondary:       get("fgSecondary"),
		Tertiary:          get("tertiary"),
		Inverse:           get("inverse"),
		ContrastPrimary:   get("contrastPrimary"),
		ContrastSecondary: get("contrastSecondary"),
		ScrollBar:         get("scrollBar"),
	}, errs
}

// Apply applies the theme to cview.Styles.
func (t Theme) Apply() {
	cview.Styles.TitleColor = tcell.GetColor(t.Title)
	cview.Styles.BorderColor = tcell.GetColor(t.Border)
	cview.Styles.GraphicsColor = tcell.GetColor(t.Graphics)

	cview.Styles.PrimaryTextColor = tcell.GetColor(t.FgPrimary)
	cview.Styles.SecondaryTextColor = tcell.GetColor(t.FgSecondary)
	cview.Styles.TertiaryTextColor = tcell.GetColor(t.Tertiary)
	cview.Styles.InverseTextColor = tcell.GetColor(t.Inverse)
	cview.Styles.ContrastPrimaryTextColor = tcell.GetColor(t.ContrastPrimary)
	cview.Styles.ContrastSecondaryTextColor = tcell.GetColor(t.ContrastSecondary)

	cview.Styles.PrimitiveBackgroundColor = tcell.GetColor(t.BgPrimary)
	cview.Styles.ContrastBackgroundColor = tcell.GetColor(t.BgContrast)
	cview.Styles.MoreContrastBackgroundColor = tcell.GetColor(t.BgMoreContrast)

	cview.Styles.ScrollBarColor = tcell.GetColor(t.ScrollBar)
}
