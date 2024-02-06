package pongo2

type Priority struct {
	Value       float64
	ClauseIndex int
	LoopStack   []int
}

type INodeStateful interface {
	INode
	CopyState(parser *Parser)
	PriorityStack() ([]Priority, *Error)
}

type nodeStateful struct {
	INode
	priorityEvalStack []PriorityEvaluator
}

func (n *nodeStateful) CopyState(parser *Parser) {
	n.priorityEvalStack = parser.PriorityStack()
}

func (n *nodeStateful) PriorityStack() ([]Priority, *Error) {
	priorityStack := make([]Priority, len(n.priorityEvalStack))
	for i, p := range n.priorityEvalStack {
		priorityValue, err := p.Priority()
		if err != nil {
			return nil, err
		}
		priorityStack[i] = Priority{
			ClauseIndex: p.ClauseIndex(),
			Value:       priorityValue,
			LoopStack:   p.LoopStack(),
		}
	}
	return priorityStack, nil
}
