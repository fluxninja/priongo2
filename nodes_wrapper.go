package pongo2

type NodeWrapper struct {
	Endtag string
	nodes  []INodeStateful
}

func (wrapper *NodeWrapper) Execute(ctx *ExecutionContext, writer TemplateWriter) *Error {
	for _, n := range wrapper.nodes {
		err := writer.SetChunkContext(ctx, n)
		if err != nil {
			return err
		}
		err = n.Execute(ctx, writer)
		if err != nil {
			return err
		}
	}
	return nil
}
