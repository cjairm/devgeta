package tuicomponents

import lip "charm.land/lipgloss/v2"

// Palette holds all lipgloss styles for the TUI. Raw fields are used directly by
// callers; method receivers on Palette encapsulate multi-step rendering logic.
type Palette struct {
	// Session state colors — used by StatusDot / StatusGlyph
	Running     lip.Style // ANSI 2 green
	NeedsReview lip.Style // ANSI 5 purple
	Dirty       lip.Style // ANSI 3 yellow
	NoSession   lip.Style // ANSI 8 gray
	Blocked     lip.Style // ANSI 1 red
	Error       lip.Style // ANSI 1 red, bold

	// Branch glyph
	BranchGlyph lip.Style // ANSI 8 dim

	// Diff colors
	DiffAdded      lip.Style // ANSI 2
	DiffRemoved    lip.Style // ANSI 1
	DiffFiles      lip.Style // ANSI 15 white
	DiffFileHeader lip.Style // bold ANSI 15 — per-file header in a diff pane

	// Tree structure
	RepoHeader  lip.Style // ANSI 6 bold
	SectionHead lip.Style // ANSI 8 dim caps

	// Row selection
	Selected lip.Style // bg ANSI 4, fg ANSI 15, bold
	Armed    lip.Style // bg ANSI 1, fg ANSI 15

	// Tabs
	TabActive   lip.Style // bold + underline + ANSI 15
	TabInactive lip.Style // ANSI 8

	// Hint bar
	HintKey  lip.Style // bold ANSI 6
	HintSep  lip.Style // ANSI 8
	HintDesc lip.Style // ANSI 8

	// Status bar mode badges
	ModeNormal  lip.Style // bg ANSI 4, fg ANSI 15, bold, pad 0 1
	ModeCommand lip.Style // bg ANSI 3, fg ANSI 0,  bold, pad 0 1
	ModeInsert  lip.Style // bg ANSI 2, fg ANSI 0,  bold, pad 0 1

	// Notification toast
	ToastBorder lip.Style // ANSI 5
	ToastTitle  lip.Style // bold ANSI 5
	ToastBody   lip.Style // ANSI 8
	ToastAction lip.Style // ANSI 6

	// Command palette / which-key
	PaletteInput    lip.Style // ANSI 15
	PaletteSelected lip.Style // bg ANSI 4, fg ANSI 15
	PaletteHint     lip.Style // ANSI 8
	PaletteBorder   lip.Style // ANSI 8

	// Misc
	Divider   lip.Style // ANSI 8
	Inactive  lip.Style // ANSI 8, italic — placeholder / offline text
	StatusMsg lip.Style // ANSI 3
	Timestamp lip.Style // ANSI 8
}

func NewPalette() *Palette {
	return &Palette{
		Running:     lip.NewStyle().Foreground(lip.Color("10")), // bright green
		NeedsReview: lip.NewStyle().Foreground(lip.Color("5")),
		Dirty:       lip.NewStyle().Foreground(lip.Color("3")),
		NoSession:   lip.NewStyle().Foreground(lip.Color("8")),
		Blocked:     lip.NewStyle().Foreground(lip.Color("1")),
		Error:       lip.NewStyle().Bold(true).Foreground(lip.Color("1")),
		BranchGlyph: lip.NewStyle().Foreground(lip.Color("8")),

		DiffAdded:      lip.NewStyle().Foreground(lip.Color("2")),
		DiffRemoved:    lip.NewStyle().Foreground(lip.Color("1")),
		DiffFiles:      lip.NewStyle().Foreground(lip.Color("15")),
		DiffFileHeader: lip.NewStyle().Bold(true).Foreground(lip.Color("15")),

		RepoHeader:  lip.NewStyle().Bold(true).Foreground(lip.Color("6")),
		SectionHead: lip.NewStyle().Foreground(lip.Color("8")),

		Selected: lip.NewStyle().Background(lip.Color("4")).Foreground(lip.Color("15")),
		Armed:    lip.NewStyle().Background(lip.Color("1")).Foreground(lip.Color("15")),

		TabActive:   lip.NewStyle().Bold(true).Underline(true).Foreground(lip.Color("15")),
		TabInactive: lip.NewStyle().Foreground(lip.Color("8")),

		HintKey:  lip.NewStyle().Bold(true).Foreground(lip.Color("6")),
		HintSep:  lip.NewStyle().Foreground(lip.Color("8")),
		HintDesc: lip.NewStyle().Foreground(lip.Color("8")),

		ModeNormal: lip.NewStyle().
			Background(lip.Color("4")).
			Foreground(lip.Color("15")).
			Bold(true).
			Padding(0, 1),
		ModeCommand: lip.NewStyle().
			Background(lip.Color("3")).
			Foreground(lip.Color("0")).
			Bold(true).
			Padding(0, 1),
		ModeInsert: lip.NewStyle().
			Background(lip.Color("2")).
			Foreground(lip.Color("0")).
			Bold(true).
			Padding(0, 1),

		ToastBorder: lip.NewStyle().Foreground(lip.Color("5")),
		ToastTitle:  lip.NewStyle().Bold(true).Foreground(lip.Color("5")),
		ToastBody:   lip.NewStyle().Foreground(lip.Color("8")),
		ToastAction: lip.NewStyle().Foreground(lip.Color("6")),

		PaletteInput:    lip.NewStyle().Foreground(lip.Color("15")),
		PaletteSelected: lip.NewStyle().Background(lip.Color("4")).Foreground(lip.Color("15")),
		PaletteHint:     lip.NewStyle().Foreground(lip.Color("8")),
		PaletteBorder:   lip.NewStyle().Foreground(lip.Color("8")),

		Divider:   lip.NewStyle().Foreground(lip.Color("8")),
		Inactive:  lip.NewStyle().Foreground(lip.Color("8")).Italic(true),
		StatusMsg: lip.NewStyle().Foreground(lip.Color("3")),
		Timestamp: lip.NewStyle().Foreground(lip.Color("8")),
	}
}
