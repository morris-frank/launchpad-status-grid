package tui

import (
	"image/color"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// AnimationInterval is how often the preview repaints itself.
//
// The device renders flash and pulse from its own clock, so the board sends no
// frames while they run — which means a terminal that only repainted on a new
// frame would show them as static. This tick is the terminal doing the device's
// job, and it is the one place in the whole context where an animation costs
// something per frame.
const AnimationInterval = 80 * time.Millisecond

// Model is the preview.
type Model struct {
	dev   *Device
	board *app.Board
	now   func() time.Time

	frame  domain.Frame
	row    int
	col    int
	width  int
	height int

	// followed is the last press the cursor was moved to, so that a press
	// already under the cursor does not fight a person moving it.
	followed    domain.Pad
	hasFollowed bool

	// unlit is the ink of a dark pad; see style.go.
	unlit color.Color
}

// Option configures a Model.
type Option func(*Model)

// New builds the preview over dev and board. The board is read for its screen
// names and its last note only; the picture comes from the frames dev is handed,
// exactly as the hardware would receive them.
func New(dev *Device, board *app.Board, opts ...Option) *Model {
	m := &Model{dev: dev, board: board, now: time.Now, frame: domain.Blank(), width: 80, height: 24, unlit: track}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

type frameMsg domain.Frame

type animateMsg time.Time

// Init satisfies bubbletea.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.awaitFrame(), tea.Tick(AnimationInterval, func(t time.Time) tea.Msg { return animateMsg(t) }))
}

// awaitFrame blocks on the device's frame channel off the UI goroutine, which
// is how a bubbletea program consumes something that is not a tea.Msg.
func (m *Model) awaitFrame() tea.Cmd {
	return func() tea.Msg {
		select {
		case f := <-m.dev.frames:
			return frameMsg(f)
		case <-m.dev.closed:
			return tea.Quit()
		}
	}
}

// Update satisfies bubbletea.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case frameMsg:
		m.frame = domain.Frame(msg)
		m.follow()
		return m, m.awaitFrame()

	case animateMsg:
		m.follow()
		return m, tea.Tick(AnimationInterval, func(t time.Time) tea.Msg { return animateMsg(t) })

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if quits(msg.String()) {
		return m, tea.Quit
	}
	m.press(msg.String())
	return m, nil
}

// press answers the preview's pad keys — moving the cursor, pressing the pad
// under it, and the two arrow buttons — and reports whether key was one. It is
// the preview's keymap without its quit keys.
func (m *Model) press(key string) bool {
	switch key {
	case "up", "k":
		m.row = clamp(m.row-1, 0, domain.GridSize)
	case "down", "j":
		m.row = clamp(m.row+1, 0, domain.GridSize)
	case "left", "h":
		m.col = clamp(m.col-1, 0, domain.GridSize)
	case "right", "l":
		m.col = clamp(m.col+1, 0, domain.GridSize)

	case "enter", " ", "space":
		if pad, ok := padAt(m.row, m.col); ok && pad.Pressable() {
			m.dev.Press(pad, m.now())
		}

	case "tab", "shift+tab":
		// The two arrow buttons, reached from the keyboard. This presses the
		// pad rather than moving the board directly: the terminal implements
		// the same port as the device, and a shortcut that skipped the port
		// would exercise a path the hardware does not have.
		n := app.NextTab
		if key == "shift+tab" {
			n = app.PrevTab
		}
		if pad, ok := domain.Top(n); ok {
			m.row, m.col = 0, n-1
			m.dev.Press(pad, m.now())
		}
	default:
		return false
	}
	return true
}

// follow moves the cursor onto the pad that was last pressed, wherever the
// press came from.
//
// This is what makes the preview a twin rather than a second board. With the
// hardware and the terminal mirrored, a thumb on a pad moves the cursor here
// and the line under the picture then names what was just pressed — which is
// the only way the device ever answers "what is this one", short of pressing
// it and finding out.
//
// Moving the cursor rather than adding a second marker is deliberate: one
// pointer that is sometimes moved for you beats two pointers you have to tell
// apart at a glance.
func (m *Model) follow() {
	status := m.board.Status()
	if !status.Pressed || (m.hasFollowed && status.Pad == m.followed) {
		return
	}
	m.followed, m.hasFollowed = status.Pad, true
	if row, col, ok := cellOf(status.Pad); ok {
		m.row, m.col = row, col
	}
}

// cellOf is padAt inverted: where in the drawn surface a pad sits.
func cellOf(p domain.Pad) (row, col int, ok bool) {
	if row, col, ok := p.RowCol(); ok {
		return row + 1, col, true
	}
	if p == domain.Logo {
		return 0, domain.GridSize, true
	}
	for n := 1; n <= domain.GridSize; n++ {
		if top, _ := domain.Top(n); top == p {
			return 0, n - 1, true
		}
		if side, _ := domain.Side(n); side == p {
			return n, domain.GridSize, true
		}
	}
	return 0, 0, false
}

// padAt is the physical layout: the top row of round buttons above the grid,
// the side column to its right, and the logo in the corner where the two chrome
// runs meet.
func padAt(row, col int) (domain.Pad, bool) {
	switch {
	case row == 0 && col == domain.GridSize:
		return domain.Logo, true
	case row == 0:
		return domain.Top(col + 1)
	case col == domain.GridSize:
		return domain.Side(row)
	default:
		return domain.Grid(row-1, col)
	}
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}
