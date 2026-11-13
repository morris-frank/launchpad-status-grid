package worker

import (
	"encoding/json"
	"testing"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
)

// A repository's pad is its main worktree's state: dirtiness only where it
// was measured, its upstream's divergence, and unreadable where the worker
// carried an error — never clean.
func TestARepositorysPadIsItsMainWorktree(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		json string
		want app.Repo
	}{
		"dirty and ahead": {
			`{"Name":"a","Branches":[{"Worktree":{"Main":false,"Dirty":9,"DirtyKnown":true}},{"Upstream":{"Ahead":2,"Behind":1},"Worktree":{"Main":true,"Dirty":3,"DirtyKnown":true}}],"Err":null}`,
			app.Repo{Name: "a", Dirty: true, Ahead: 2, Behind: 1},
		},
		"dirtiness unmeasured": {
			`{"Name":"b","Branches":[{"Worktree":{"Main":true,"Dirty":3,"DirtyKnown":false}}],"Err":null}`,
			app.Repo{Name: "b"},
		},
		"unreadable": {
			`{"Name":"c","Branches":null,"Err":{}}`,
			app.Repo{Name: "c", Unreadable: true},
		},
	} {
		var r repo
		if err := json.Unmarshal([]byte(tc.json), &r); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := padRepo(r); got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}
