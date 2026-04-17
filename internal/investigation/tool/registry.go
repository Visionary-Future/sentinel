package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sentinelai/sentinel/internal/llm"
)

// Func is the signature every tool implementation must satisfy.
// input is the raw JSON provided by the LLM.
type Func func(ctx context.Context, input json.RawMessage) (string, error)

// Registry maps tool names to their implementations and schemas.
type Registry struct {
	tools map[string]entry
}

type entry struct {
	tool llm.Tool
	fn   Func
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]entry)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t llm.Tool, fn Func) {
	r.tools[t.Name] = entry{tool: t, fn: fn}
}

// Tools returns all registered tools as LLM tool definitions.
func (r *Registry) Tools() []llm.Tool {
	result := make([]llm.Tool, 0, len(r.tools))
	for _, e := range r.tools {
		result = append(result, e.tool)
	}
	return result
}

// Execute calls the named tool with the given input.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	e, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return e.fn(ctx, input)
}
