package pongo2

// The root document
type nodeDocument struct {
	Nodes []INodeStateful
}

func (doc *nodeDocument) Execute(ctx *ExecutionContext, writer TemplateWriter) *Error {
	for _, n := range doc.Nodes {
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
