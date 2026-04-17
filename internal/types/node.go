package types

import "encoding/json"

type Node struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

const (
	NodeHTTPRequest  = "http_request"
	NodeIf           = "if"
	NodeSwitch       = "switch"
	NodeForEach      = "for_each"
	NodeSetVariable  = "set_variable"
	NodeLLMCall      = "llm_call"
	NodeSendTelegram = "send_telegram"
	NodeSendEmail    = "send_email"
	NodeRunJS        = "run_js"
	NodeWait         = "wait"
	NodeLog          = "log"
	NodeSubWorkflow  = "sub_workflow"
	NodeStop         = "stop"
)
