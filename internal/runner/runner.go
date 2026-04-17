package runner

import (
	"context"
	"encoding/json"

	"orbyt-flow/internal/template"
	"orbyt-flow/internal/types"
)

// Input is passed to every Runner.
type Input struct {
	Config  json.RawMessage
	Context *template.Context
}

// Output is returned by every Runner.
type Output struct {
	Data json.RawMessage
}

// Runner executes a single node.
type Runner interface {
	Run(ctx context.Context, input Input) (*Output, error)
}

// Registry returns a map of node type → Runner implementation.
func Registry() map[string]Runner {
	return map[string]Runner{
		types.NodeHTTPRequest:  &HTTPRequestRunner{},
		types.NodeLog:          &LogRunner{},
		types.NodeSetVariable:  &SetVariableRunner{},
		types.NodeWait:         &WaitRunner{},
		types.NodeStop:         &StopRunner{},
		types.NodeIf:           &IfRunner{},
		types.NodeSendTelegram: &SendTelegramRunner{},
		types.NodeLLMCall:      &LLMCallRunner{},
	}
}

// marshalOutput is a convenience helper for building Output.Data from a map.
func marshalOutput(v any) (*Output, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &Output{Data: b}, nil
}
