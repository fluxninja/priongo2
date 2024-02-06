package pongo2

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

type TrackingIndex struct {
	Index int
	Set   bool
}

type PriorityDetails struct {
	Value       float64
	ClauseIndex int
	LoopStack   map[int]int
}

type Chunk struct {
	Value         string
	Priorities    []PriorityDetails
	TrackingIndex TrackingIndex
}

type Chunks []Chunk

type TemplateWriter interface {
	io.Writer
	WriteString(string) (int, error)
	SetChunkContext(*ExecutionContext, INodeStateful) *Error
	SetTrackingIndex(int)
	UnsetTrackingIndex()
	Chunks() Chunks
}

type templateWriterIO struct {
	w io.Writer
	chunkWriter
}

func (tw *templateWriterIO) WriteString(s string) (int, error) {
	tw.WriteChunk(s)

	return tw.w.Write([]byte(s))
}

func (tw *templateWriterIO) Write(b []byte) (int, error) {
	return tw.w.Write(b)
}

type templateWriterBuffer struct {
	b *bytes.Buffer
	chunkWriter
}

func (tw *templateWriterBuffer) WriteString(s string) (int, error) {
	tw.WriteChunk(s)

	return tw.b.WriteString(s)
}

func (tw *templateWriterBuffer) Write(b []byte) (int, error) {
	return tw.b.Write(b)
}

type chunkWriter struct {
	chunks        Chunks
	priorities    []PriorityDetails
	trackingIndex TrackingIndex
}

func (cw *chunkWriter) SetChunkContext(ctx *ExecutionContext, nodeStateful INodeStateful) *Error {
	cw.priorities = nil

	priorityStack, err := nodeStateful.PriorityStack()
	if err != nil {
		return err
	}

	loopPositions := make(map[int]int)
	for forLoop := ctx.Private["forloop"]; forLoop != nil; forLoop = forLoop.(*tagForLoopInformation).Parentloop {
		loopPositions[forLoop.(*tagForLoopInformation).LoopIndex] = forLoop.(*tagForLoopInformation).Counter0
	}

	for _, p := range priorityStack {
		pd := &PriorityDetails{
			Value:       p.Value,
			ClauseIndex: p.ClauseIndex,
		}
		for _, l := range p.LoopStack {
			if pos, ok := loopPositions[l]; ok {
				pd.LoopStack[l] = pos
			} else {
				// return error
				return &Error{
					Sender:    "SetChunkContext",
					OrigError: fmt.Errorf("Loop iteration position not found for loop index %d", l),
				}
			}
		}
		cw.priorities = append(cw.priorities, *pd)
	}

	return nil
}

func (cw *chunkWriter) SetTrackingIndex(i int) {
	cw.trackingIndex.Index = i
	cw.trackingIndex.Set = true
}

func (cw *chunkWriter) UnsetTrackingIndex() {
	cw.trackingIndex.Set = false
}

func (cw *chunkWriter) Chunks() Chunks {
	return cw.chunks
}

func (cw *chunkWriter) WriteChunk(s string) {
	cw.chunks = append(cw.chunks,
		Chunk{
			Value:         s,
			Priorities:    cw.priorities,
			TrackingIndex: cw.trackingIndex,
		},
	)
}

type Template struct {
	set *TemplateSet

	// Input
	isTplString bool
	name        string
	tpl         string
	size        int

	// Calculation
	tokens []*Token
	parser *Parser

	// first come, first serve (it's important to not override existing entries in here)
	level          int
	parent         *Template
	child          *Template
	blocks         map[string]*NodeWrapper
	exportedMacros map[string]*tagMacroNode

	// Output
	root *nodeDocument

	// Options allow you to change the behavior of template-engine.
	// You can change the options before calling the Execute method.
	Options *Options
}

func newTemplateString(set *TemplateSet, tpl []byte) (*Template, error) {
	return newTemplate(set, "<string>", true, tpl)
}

func newTemplate(set *TemplateSet, name string, isTplString bool, tpl []byte) (*Template, error) {
	strTpl := string(tpl)

	// Create the template
	t := &Template{
		set:            set,
		isTplString:    isTplString,
		name:           name,
		tpl:            strTpl,
		size:           len(strTpl),
		blocks:         make(map[string]*NodeWrapper),
		exportedMacros: make(map[string]*tagMacroNode),
		Options:        newOptions(),
	}
	// Copy all settings from another Options.
	t.Options.Update(set.Options)

	// Tokenize it
	tokens, err := lex(name, strTpl)
	if err != nil {
		return nil, err
	}
	t.tokens = tokens

	// For debugging purposes, show all tokens:
	/*for i, t := range tokens {
		fmt.Printf("%3d. %s\n", i, t)
	}*/

	// Parse it
	err = t.parse()
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (tpl *Template) newContextForExecution(context Context) (*Template, *ExecutionContext, error) {
	if tpl.Options.TrimBlocks || tpl.Options.LStripBlocks {
		// Issue #94 https://github.com/flosch/pongo2/issues/94
		// If an application configures pongo2 template to trim_blocks,
		// the first newline after a template tag is removed automatically (like in PHP).
		prev := &Token{
			Typ: TokenHTML,
			Val: "\n",
		}

		for _, t := range tpl.tokens {
			if tpl.Options.LStripBlocks {
				if prev.Typ == TokenHTML && t.Typ != TokenHTML && t.Val == "{%" {
					prev.Val = strings.TrimRight(prev.Val, "\t ")
				}
			}

			if tpl.Options.TrimBlocks {
				if prev.Typ != TokenHTML && t.Typ == TokenHTML && prev.Val == "%}" {
					if len(t.Val) > 0 && t.Val[0] == '\n' {
						t.Val = t.Val[1:len(t.Val)]
					}
				}
			}

			prev = t
		}
	}

	// Determine the parent to be executed (for template inheritance)
	parent := tpl
	for parent.parent != nil {
		parent = parent.parent
	}

	// Create context if none is given
	newContext := make(Context)
	newContext.Update(tpl.set.Globals)

	if context != nil {
		newContext.Update(context)

		if len(newContext) > 0 {
			// Check for context name syntax
			err := newContext.checkForValidIdentifiers()
			if err != nil {
				return parent, nil, err
			}

			// Check for clashes with macro names
			for k := range newContext {
				_, has := tpl.exportedMacros[k]
				if has {
					return parent, nil, &Error{
						Filename:  tpl.name,
						Sender:    "execution",
						OrigError: fmt.Errorf("context key name '%s' clashes with macro '%s'", k, k),
					}
				}
			}
		}
	}

	// Create operational context
	ctx := newExecutionContext(parent, newContext)

	return parent, ctx, nil
}

func (tpl *Template) execute(context Context, writer TemplateWriter) error {
	parent, ctx, err := tpl.newContextForExecution(context)
	if err != nil {
		return err
	}

	// Run the selected document
	if err := parent.root.Execute(ctx, writer); err != nil {
		return err
	}

	return nil
}

func (tpl *Template) newTemplateWriterAndExecute(context Context, writer io.Writer) error {
	return tpl.execute(context, &templateWriterIO{w: writer})
}

func (tpl *Template) newBufferAndExecute(context Context) (*bytes.Buffer, Chunks, error) {
	// Create output buffer
	// We assume that the rendered template will be 30% larger
	buffer := bytes.NewBuffer(make([]byte, 0, int(float64(tpl.size)*1.3)))
	twb := &templateWriterBuffer{b: buffer}
	if err := tpl.execute(context, twb); err != nil {
		return nil, nil, err
	}
	return buffer, twb.Chunks(), nil
}

// Executes the template with the given context and writes to writer (io.Writer)
// on success. Context can be nil. Nothing is written on error; instead the error
// is being returned.
func (tpl *Template) ExecuteWriter(context Context, writer io.Writer) error {
	buf, _, err := tpl.newBufferAndExecute(context)
	if err != nil {
		return err
	}
	_, err = buf.WriteTo(writer)
	if err != nil {
		return err
	}
	return nil
}

// Same as ExecuteWriter. The only difference between both functions is that
// this function might already have written parts of the generated template in the
// case of an execution error because there's no intermediate buffer involved for
// performance reasons. This is handy if you need high performance template
// generation or if you want to manage your own pool of buffers.
func (tpl *Template) ExecuteWriterUnbuffered(context Context, writer io.Writer) error {
	return tpl.newTemplateWriterAndExecute(context, writer)
}

// Executes the template and returns the rendered template as a []byte
func (tpl *Template) ExecuteBytes(context Context) ([]byte, error) {
	// Execute template
	buffer, _, err := tpl.newBufferAndExecute(context)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Executes the template and returns the rendered template as a WriteChunks
func (tpl *Template) ExecuteChunks(context Context) (Chunks, error) {
	// Execute template
	_, chunks, err := tpl.newBufferAndExecute(context)
	if err != nil {
		return nil, err
	}

	return chunks, nil
}

// Executes the template and returns the rendered template as a string
func (tpl *Template) Execute(context Context) (string, error) {
	// Execute template
	buffer, _, err := tpl.newBufferAndExecute(context)
	if err != nil {
		return "", err
	}

	return buffer.String(), nil
}

func (tpl *Template) ExecuteBlocks(context Context, blocks []string) (map[string]string, error) {
	var parents []*Template
	result := make(map[string]string)

	parent := tpl
	for parent != nil {
		// We only want to execute the template if it has a block we want
		for _, block := range blocks {
			if _, ok := tpl.blocks[block]; ok {
				parents = append(parents, parent)
				break
			}
		}
		parent = parent.parent
	}

	for _, t := range parents {
		var buffer *bytes.Buffer
		var ctx *ExecutionContext
		var err error
		for _, blockName := range blocks {
			if _, ok := result[blockName]; ok {
				continue
			}
			if blockWrapper, ok := t.blocks[blockName]; ok {
				// assign the buffer if we haven't done so
				if buffer == nil {
					buffer = bytes.NewBuffer(make([]byte, 0, int(float64(t.size)*1.3)))
				}
				// assign the context if we haven't done so
				if ctx == nil {
					_, ctx, err = t.newContextForExecution(context)
					if err != nil {
						return nil, err
					}
				}
				bErr := blockWrapper.Execute(ctx, &templateWriterBuffer{b: buffer})
				if bErr != nil {
					return nil, bErr
				}
				result[blockName] = buffer.String()
				buffer.Reset()
			}
		}
		// We have found all blocks
		if len(blocks) == len(result) {
			break
		}
	}

	return result, nil
}
