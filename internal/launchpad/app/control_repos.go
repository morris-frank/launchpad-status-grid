package app

import (
	"context"
	"fmt"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// ReposScreen shows the workspace one pad per repository: what state each one
// is in, and nothing finer.
//
// One pad is a hard constraint and a useful one. A repository has a dozen facts
// worth knowing and this screen carries exactly the one that decides whether to
// go and look — so the colours are a priority order, not a set of flags, and
// what a pad shows is the most urgent true thing about that repository.
type ReposControl struct {
	data *Data[[]Repo]
}

// NewReposControl reads from src.
func NewReposControl(src Repos) *ReposControl {
	return &ReposControl{data: NewData(func(ctx context.Context) ([]Repo, error) { return src.Repos(ctx) })}
}

// Refresh implements Refresher.
func (s *ReposControl) Refresh(ctx context.Context) error { return s.data.Refresh(ctx) }

// Render implements Control.
func (s *ReposControl) Render(_ time.Time, size domain.Size) domain.Frame {
	repos, loaded, err := s.data.Value()
	if f, empty := emptyGrid(loaded, err, len(repos), size); empty {
		return f
	}

	f := domain.Blank()
	for i, repo := range repos {
		if i >= size.Count() {
			break
		}
		f = f.SetGrid(i/size.Cols, i%size.Cols, repoCell(repo))
	}
	return f
}

// repoCell is the priority order, most urgent first.
//
// Unreadable is red and flashing because it is the one state where the board is
// not telling you about the repository at all, and a repository the worker failed to
// read must never be able to look clean. Dirty outranks divergence because
// uncommitted work is the thing that gets lost.
func repoCell(r Repo) domain.Cell {
	switch {
	case r.Unreadable:
		return domain.Red.Cell(domain.LevelFull).With(domain.BFlash(0))
	case r.Dirty:
		return domain.Amber.Cell(domain.LevelFull)
	case r.Ahead > 0:
		return domain.Cyan.Cell(domain.LevelBright)
	case r.Behind > 0:
		return domain.Yellow.Cell(domain.LevelDim)
	default:
		return domain.Green.Cell(domain.LevelDim)
	}
}

// Press names the repository and what its pad is saying. The screen takes no
// action on a checkout: a board that refreshes itself every few seconds and
// mutates repositories on a keystroke is a board whose worst bug is
// destructive, which is the same boundary the terminal dashboard holds.
func (s *ReposControl) Press(_ context.Context, at Slot) (Outcome, error) {
	repos, _, _ := s.data.Value()
	i := at.Index()
	if i >= len(repos) {
		return Outcome{Attention: true}, nil
	}
	return Outcome{Note: fmt.Sprintf("%s — %s", repos[i].Name, repoState(repos[i]))}, nil
}

func repoState(r Repo) string {
	switch {
	case r.Unreadable:
		return "could not be read"
	case r.Dirty:
		return "uncommitted changes"
	case r.Ahead > 0 && r.Behind > 0:
		return fmt.Sprintf("%d ahead, %d behind", r.Ahead, r.Behind)
	case r.Ahead > 0:
		return fmt.Sprintf("%d ahead", r.Ahead)
	case r.Behind > 0:
		return fmt.Sprintf("%d behind", r.Behind)
	default:
		return "clean"
	}
}

// Describe implements Describer, saying the same thing a press would. That is
// deliberate for this control in particular: a press here takes no action at
// all, so if the two lines differed one of them would be wrong.
func (s *ReposControl) Describe(_ time.Time, at Slot) string {
	repos, loaded, err := s.data.Value()
	if note, pending := pending(loaded, err); pending {
		return note
	}
	i := at.Index()
	if i >= len(repos) {
		return "no repository on this pad"
	}
	return fmt.Sprintf("%s — %s", repos[i].Name, repoState(repos[i]))
}
