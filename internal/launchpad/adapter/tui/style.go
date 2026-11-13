package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The preview's own inks. mario draws this surface through its theme package;
// a standalone binary has no brand scheme to follow, so it keeps the few roles
// the preview needs here and nothing more. The pads themselves are never one
// of these: their hue is the hardware's (lamp in view.go).
var (
	accent = lipgloss.Color("#B5E61D")
	faint  = lipgloss.Color("#7A7F80")
	// track is the unlit pad: the device draws nothing there, so the
	// terminal's dim grey stands in.
	track = lipgloss.Color("#2E3233")
)

// The characters the cursor is made of, so the hovered pad is found with
// colour stripped (NO_COLOR) as readily as with it.
const (
	encloseOpen  = "["
	encloseClose = "]"
	selectionBar = "▌"
)

// glow draws text in an ink the device chose.
func glow(ink color.Color, bold bool, text string) string {
	return lipgloss.NewStyle().Foreground(ink).Bold(bold).Render(text)
}

func muted(text string) string { return lipgloss.NewStyle().Foreground(faint).Render(text) }

func cursor() string { return lipgloss.NewStyle().Foreground(accent).Render(selectionBar) }

// enclose replaces a cell's outer two columns with brackets in the accent.
func enclose(cell string) string {
	w := ansi.StringWidth(cell)
	bracket := lipgloss.NewStyle().Foreground(accent)
	return bracket.Render(encloseOpen) + ansi.Cut(cell, 1, w-1) + bracket.Render(encloseClose)
}

// quits is the preview's quit keys, mario's: esc is not one, because it
// backs out of things and a reflexive esc must not close the board.
func quits(key string) bool {
	switch key {
	case "q", "ctrl+c", "ctrl+q", "ctrl+d":
		return true
	}
	return false
}

// screen lays the preview out: a title and tab row, the surface, the legend
// and a shortcut line, clipped to the terminal's width and height.
func screen(title string, tabs []string, active int, content, legend, shortcuts string, width, height int) string {
	var head strings.Builder
	head.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	for i, t := range tabs {
		if i == active {
			head.WriteString("  " + cursor() + lipgloss.NewStyle().Bold(true).Render(t))
			continue
		}
		head.WriteString("   " + muted(t))
	}
	lines := []string{head.String(), ""}
	lines = append(lines, strings.Split(strings.TrimRight(content, "\n"), "\n")...)
	lines = append(lines, "")
	lines = append(lines, strings.Split(legend, "\n")...)
	lines = append(lines, "", muted(shortcuts))
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
}
