package pongo2

import "fmt"

type PriorityEvaluator interface {
	Priority() (float64, *Error)
	Evaluate(*ExecutionContext) *Error
	SetClauseIndex(int)
	ClauseIndex() int
	SetLoopStack([]int)
	LoopStack() []int
}

type priorityEvaluator struct {
	priority    float64
	evaluator   IEvaluator
	evaluated   bool
	clauseIndex int
	loopStack   []int
}

// priorityEvaluator implements PriorityEvaluator
var _ PriorityEvaluator = &priorityEvaluator{}

func (pe *priorityEvaluator) Priority() (float64, *Error) {
	if !pe.evaluated {
		return 0, &Error{
			Sender:    "priority",
			OrigError: fmt.Errorf("Priority not evaluated yet"),
		}
	}
	return pe.priority, nil
}

func (pe *priorityEvaluator) Evaluate(ctx *ExecutionContext) *Error {
	val, err := pe.evaluator.Evaluate(ctx)
	if err != nil {
		return err
	}

	pe.priority = val.Float()
	pe.evaluated = true

	return nil
}

func (pe *priorityEvaluator) SetClauseIndex(i int) {
	pe.clauseIndex = i
}

func (pe *priorityEvaluator) ClauseIndex() int {
	return pe.clauseIndex
}

func (pe *priorityEvaluator) SetLoopStack(loopStack []int) {
	pe.loopStack = loopStack
}

func (pe *priorityEvaluator) LoopStack() []int {
	return pe.loopStack
}

type tagPriorityNode struct {
	wrapper           *NodeWrapper
	priorityEvaluator PriorityEvaluator
}

func (node *tagPriorityNode) Execute(ctx *ExecutionContext, writer TemplateWriter) *Error {
	err := node.priorityEvaluator.Evaluate(ctx)
	if err != nil {
		return err
	}

	return node.wrapper.Execute(ctx, writer)
}

func tagPriorityParser(parser *Parser, _ *Token, arguments *Parser) (INodeTag, *Error) {
	// Parse pEval
	pEval, err := arguments.ParseExpression()
	if err != nil {
		return nil, err
	}

	priorityNode := &tagPriorityNode{
		priorityEvaluator: &priorityEvaluator{
			evaluator: pEval,
		},
	}

	if arguments.Remaining() > 0 {
		return nil, arguments.Error("priority is malformed.", nil)
	}

	parser.PushPriority(priorityNode.priorityEvaluator)

	wrapper, endargs, err := parser.WrapUntilTag("endpriority")
	if err != nil {
		return nil, err
	}
	priorityNode.wrapper = wrapper

	parser.PopPriority()

	if endargs.Count() > 0 {
		return nil, endargs.Error("Arguments not allowed here.", nil)
	}

	return priorityNode, nil
}

func init() {
	RegisterTag("priority", tagPriorityParser)
}
