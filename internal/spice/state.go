package spice

import "fmt"

// State is where a run is: queued, running, then done, failed or
// cancelled; a finished run goes stale when its simulation changes.
type State string

// Run states.
const (
	Queued    State = "queued"
	Running   State = "running"
	Done      State = "done"
	Failed    State = "failed"
	Cancelled State = "cancelled"
	Stale     State = "stale"
)

// transitions are the moves a run may make.
var transitions = map[State][]State{
	Queued:  {Running, Cancelled},
	Running: {Done, Failed, Cancelled},
	Done:    {Stale},
	Failed:  {Stale},
}

// To is the state a run moves to, or why it can't.
func (s State) To(next State) (State, error) {
	for _, t := range transitions[s] {
		if t == next {
			return next, nil
		}
	}
	return s, fmt.Errorf("a %s run can't become %s", s, next)
}

// Finished reports whether a run is over.
func (s State) Finished() bool {
	return s == Done || s == Failed || s == Cancelled || s == Stale
}
