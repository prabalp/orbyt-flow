package template_test

import (
	"encoding/json"
	"testing"

	"orbyt-flow/internal/template"
)

func baseCtx() *template.Context {
	return &template.Context{
		NodeOutputs: map[string]interface{}{
			"n1": map[string]interface{}{
				"output": map[string]interface{}{
					"body":  "hello world",
					"code":  200,
					"items": []interface{}{"first", "second", "third"},
					"nested": map[string]interface{}{
						"deep": "value",
					},
				},
			},
		},
		Env: map[string]string{
			"TELEGRAM_CHAT_ID": "12345",
			"API_KEY":          "secret",
		},
		Vars: map[string]interface{}{
			"my_variable": "my_value",
			"count":       42,
		},
	}
}

func TestResolveEnvVar(t *testing.T) {
	ctx := baseCtx()

	got, err := template.Resolve("chat={{env.TELEGRAM_CHAT_ID}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "chat=12345" {
		t.Errorf("got %q, want %q", got, "chat=12345")
	}

	// Multiple env tokens in one string.
	got, err = template.Resolve("{{env.TELEGRAM_CHAT_ID}}:{{env.API_KEY}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "12345:secret" {
		t.Errorf("got %q, want %q", got, "12345:secret")
	}
}

func TestResolveNodeOutput(t *testing.T) {
	ctx := baseCtx()

	got, err := template.Resolve("body={{n1.output.body}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "body=hello world" {
		t.Errorf("got %q, want %q", got, "body=hello world")
	}
}

func TestResolveNestedPath(t *testing.T) {
	ctx := baseCtx()

	got, err := template.Resolve("{{n1.output.nested.deep}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
}

func TestResolveArrayIndex(t *testing.T) {
	ctx := baseCtx()

	got, err := template.Resolve("{{n1.output.items.0}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "first" {
		t.Errorf("[0]: got %q, want %q", got, "first")
	}

	got, err = template.Resolve("{{n1.output.items.2}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "third" {
		t.Errorf("[2]: got %q, want %q", got, "third")
	}
}

func TestResolveJSON(t *testing.T) {
	ctx := baseCtx()

	raw := json.RawMessage(`{
		"chat_id": "{{env.TELEGRAM_CHAT_ID}}",
		"text": "result: {{n1.output.body}}",
		"meta": {
			"key": "{{vars.my_variable}}",
			"items": ["{{n1.output.items.1}}", "static"]
		}
	}`)

	out, err := template.ResolveJSON(raw, ctx)
	if err != nil {
		t.Fatalf("ResolveJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result["chat_id"] != "12345" {
		t.Errorf("chat_id: got %v, want 12345", result["chat_id"])
	}
	if result["text"] != "result: hello world" {
		t.Errorf("text: got %v, want \"result: hello world\"", result["text"])
	}
	meta := result["meta"].(map[string]interface{})
	if meta["key"] != "my_value" {
		t.Errorf("meta.key: got %v, want my_value", meta["key"])
	}
	items := meta["items"].([]interface{})
	if items[0] != "second" {
		t.Errorf("meta.items[0]: got %v, want second", items[0])
	}
	if items[1] != "static" {
		t.Errorf("meta.items[1]: got %v, want static", items[1])
	}
}

func TestMissingKeyReturnsError(t *testing.T) {
	ctx := baseCtx()

	cases := []string{
		"{{env.MISSING}}",
		"{{vars.no_such_var}}",
		"{{n1.output.no_field}}",
		"{{n99.output.body}}", // unknown node
		"{{n1.output.items.9}}", // out-of-bounds index
	}

	for _, input := range cases {
		_, err := template.Resolve(input, ctx)
		if err == nil {
			t.Errorf("expected error for %q, got nil", input)
		}
	}
}

func slackCtx() *template.Context {
	return &template.Context{
		NodeOutputs: map[string]interface{}{
			"fetch_messages": map[string]interface{}{
				"body": map[string]interface{}{
					"ok": true,
					"messages": []interface{}{
						map[string]interface{}{"text": "Hello world"},
						map[string]interface{}{"text": "ooooo yeahhh"},
						map[string]interface{}{"subtype": "channel_join", "text": "<@U123> has joined"},
					},
				},
			},
			"objnode": map[string]interface{}{
				"body": map[string]interface{}{
					"nested": map[string]interface{}{"count": 3.0},
					"arr":    []interface{}{1.0, 2.0},
				},
			},
		},
		Env:  map[string]string{},
		Vars: map[string]interface{}{},
	}
}

func TestBracketArrayIndexing(t *testing.T) {
	ctx := slackCtx()

	got, err := template.Resolve("{{fetch_messages.body.messages[0].text}}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello world" {
		t.Errorf("got %q want Hello world", got)
	}

	got, err = template.Resolve("{{fetch_messages.body.messages[-1].text}}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<@U123> has joined" {
		t.Errorf("got %q", got)
	}
}

func TestWildcardFieldJoin(t *testing.T) {
	ctx := slackCtx()

	want := "Hello world\nooooo yeahhh\n<@U123> has joined"
	got, err := template.Resolve("{{fetch_messages.body.messages[*].text}}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestBoolAndDefaultModifier(t *testing.T) {
	ctx := slackCtx()

	got, err := template.Resolve("{{fetch_messages.body.ok}}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" {
		t.Errorf("bool scalar: got %q", got)
	}

	got, err = template.Resolve(`{{fetch_messages.body.missing_key | default: "none"}}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "none" {
		t.Errorf("default: got %q", got)
	}

	got, err = template.Resolve(`{{fetch_messages.body.missing_key | default: ""}}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("default empty: got %q", got)
	}
}

func TestJSONInjectionObjectInStringField(t *testing.T) {
	ctx := slackCtx()

	raw := json.RawMessage(`{"payload": "{{objnode.body.nested}}"}`)
	out, err := template.ResolveJSON(raw, ctx)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	payload, ok := result["payload"].(string)
	if !ok {
		t.Fatalf("payload type %T", result["payload"])
	}
	var check map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &check); err != nil {
		t.Fatalf("payload is not JSON: %q err %v", payload, err)
	}
	if check["count"] != float64(3) {
		t.Errorf("payload JSON: %#v", check)
	}
}

func TestJSONFullTemplateObjectStaysJSONString(t *testing.T) {
	ctx := slackCtx()

	raw := json.RawMessage(`{"nested": "{{objnode.body.nested}}"}`)
	out, err := template.ResolveJSON(raw, ctx)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	nested, ok := result["nested"].(string)
	if !ok {
		t.Fatalf("want JSON string field, got %T", result["nested"])
	}
	var check map[string]interface{}
	if err := json.Unmarshal([]byte(nested), &check); err != nil {
		t.Fatal(err)
	}
	if check["count"] != float64(3) {
		t.Errorf("nested: %#v", check)
	}
}

func TestJSONFullTemplateArrayStaysJSONString(t *testing.T) {
	ctx := slackCtx()

	raw := json.RawMessage(`{"items": "{{objnode.body.arr}}"}`)
	out, err := template.ResolveJSON(raw, ctx)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	itemsStr, ok := result["items"].(string)
	if !ok {
		t.Fatalf("want JSON string field, got %T", result["items"])
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(itemsStr), &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 2 {
		t.Fatal(arr)
	}
}

func TestMixedStringEmbedsJSONForObjectFragment(t *testing.T) {
	ctx := slackCtx()

	raw := json.RawMessage(`{"x": "before {{objnode.body.nested}} after"}`)
	out, err := template.ResolveJSON(raw, ctx)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	x := result["x"].(string)
	if x != `before {"count":3} after` && x != `before {"count":3.0} after` {
		t.Errorf("got %q", x)
	}
}

func TestUnknownModifierErrors(t *testing.T) {
	ctx := baseCtx()
	_, err := template.Resolve(`{{env.API_KEY | noop: "x"}}`, ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveJSONFullTemplateWithDefault(t *testing.T) {
	ctx := slackCtx()
	raw := json.RawMessage(`{"x": "{{fetch_messages.body.missing_key | default: \"none\"}}"}`)
	out, err := template.ResolveJSON(raw, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if result["x"] != "none" {
		t.Fatalf("got %#v", result["x"])
	}
}
