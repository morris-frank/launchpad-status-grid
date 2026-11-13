package app

import (
	"context"
	"path"
	"time"
)

// The ports the built-in screens read, declared beside the screens that consume
// them rather than beside whatever implements them. Every one of them is
// satisfied at the composition root, which is the only place allowed to know
// that a session comes from the workspace context and a terminal is brought
// forward by a shell command.
//
// They are stated as facts and never as widgets: a source reports how many
// tokens a session is carrying, never how many pads that should light. What the
// board makes of a fact is the screen's decision, and keeping it that way is why
// a screen can be rewritten without touching anything that feeds it.

// Session is one local coding-agent session.
type Session struct {
	// Tool is the agent that owns the session, lowercase: "claude", "codex".
	Tool string
	// ID is the agent's own identifier for the session, which is what lets a
	// press act on this one rather than on whatever is newest.
	ID string
	// Dir is the working directory the session was started in.
	Dir string
	// ConfigDir is the CLAUDE_CONFIG_DIR holding the session, empty for the
	// default: resumed under another, claude finds no such session.
	ConfigDir string
	// Ended is true only where the process behind the session was measured
	// gone. A focus may reopen an ended session; one running or unmeasured is
	// never reopened, because two agents in one session is a second writer.
	Ended bool
	// LastWrite is when the session's transcript was last appended to. It is
	// the only liveness evidence available without attaching to the process,
	// and the board says exactly that much.
	LastWrite time.Time
	// ContextTokens is how much context the last recorded turn carried, or 0
	// when it is not known.
	ContextTokens int
	// ContextLimit is the window those tokens sit in, or 0 when nobody has
	// said. It is supplied by whoever wired the board rather than read from the
	// transcript, because a transcript names a model family and the window
	// belongs to a variant.
	ContextLimit int
}

// Label is the short name a board shows for this session: the working
// directory's last element, which is the repository in the layouts mario deals
// with. The full path is what a press acts on; this is what a person reads.
func (s Session) Label() string { return path.Base(s.Dir) }

// ContextRemaining is the fraction of the window still free, and false when
// there is no honest fraction to give — an unknown limit or an unread
// transcript. A board must draw nothing rather than assume a denominator.
func (s Session) ContextRemaining() (float64, bool) {
	if s.ContextLimit <= 0 || s.ContextTokens <= 0 {
		return 0, false
	}
	free := 1 - float64(s.ContextTokens)/float64(s.ContextLimit)
	if free < 0 {
		return 0, true
	}
	return free, true
}

// Sessions reports the agent sessions worth showing, newest first.
type Sessions interface {
	Sessions(ctx context.Context) ([]Session, error)
}

// Focus brings a session's terminal forward. It is the one outward action the
// built-in screens take, and the reason the sessions screen is worth a pad
// rather than a line in a dashboard.
type Focus interface {
	Focus(ctx context.Context, s Session) error
}

// Repo is one repository in the workspace, reduced to what a single pad can
// say. Anything finer is a question for mario's `cappy launch`, which has a screen the
// width of a terminal to answer it on.
type Repo struct {
	// Name is the repository's path relative to the workspace root.
	Name string
	// Dirty reports uncommitted changes in the main working tree.
	Dirty bool
	// Ahead and Behind are the checked-out branch's divergence from its
	// upstream. Both zero covers "in sync" and "no upstream configured", which
	// a pad cannot distinguish and does not try to.
	Ahead, Behind int
	// Unreadable marks a repository the worker could not read at all. It is its own
	// state rather than a zero one, because a repository that failed to load
	// must never look clean.
	Unreadable bool
}

// Repos reports the workspace's repositories.
type Repos interface {
	Repos(ctx context.Context) ([]Repo, error)
}
