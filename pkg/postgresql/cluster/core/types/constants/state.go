package pgcConstants

type State uint64

const (
	Empty State = 0
	Ready State = 1 << iota
	Pending
	Provisioning
	Configuring
	Failed
)

func (s State) Contains(state State) bool {
	return s&state == state
}

func (s State) Add(state State) State {
	return s | state
}

func (s State) Remove(state State) State {
	return s &^ state
}
