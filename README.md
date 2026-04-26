# orbyt-flow

A deterministic workflow orchestration engine with a REST API and MCP (Model Context Protocol) server. Define node-based workflows, execute them synchronously or asynchronously, and connect them directly to AI agents like Claude.

---

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Running as an HTTP Server](#running-as-an-http-server)
  - [Environment Variables](#environment-variables)
  - [Data Directory Layout](#data-directory-layout)
  - [REST API Reference](#rest-api-reference)
  - [Authentication](#authentication)
  - [User Environment Variables](#user-environment-variables)
- [Running as an MCP Server](#running-as-an-mcp-server)
  - [MCP Environment Variables](#mcp-environment-variables)
  - [MCP Tools Reference](#mcp-tools-reference)
- [Connecting to a Claude Agent](#connecting-to-a-claude-agent)
  - [Claude Desktop (claude_desktop_config.json)](#claude-desktop)
  - [Claude Code CLI](#claude-code-cli)
  - [Custom Agent via the Anthropic SDK](#custom-agent-via-the-anthropic-sdk)
- [Workflow Schema](#workflow-schema)
  - [Node Types](#node-types)
  - [Template Variables](#template-variables)
  - [Connections & Branching](#connections--branching)
  - [Error Handling & Retries](#error-handling--retries)
- [End-to-End Examples](#end-to-end-examples)
  - [HTTP API: Create and trigger a workflow](#http-api-create-and-trigger-a-workflow)
  - [MCP: Agent-driven workflow creation](#mcp-agent-driven-workflow-creation)
- [Development](#development)

---

## Overview

orbyt-flow executes workflows described as directed acyclic graphs (DAGs). Each node in the graph performs one unit of work — an HTTP request, an LLM call, a conditional branch, a Telegram message, and more. Nodes are wired together with a `connections` array and executed in topologically sorted order.

The engine ships two interfaces, both backed by the same executor:

| Mode | Transport | Use case |
|------|-----------|----------|
| **HTTP server** | JSON REST API | Programmatic or human access, webhooks, dashboards |
| **MCP server** | stdio (MCP protocol) | AI agents (Claude Desktop, Claude Code, custom SDK agents) |

---

## Quick Start

### Option 1: Docker (recommended for VPS deployment)

```bash
# Build and run with Docker Compose
docker compose up -d

# Or build and run manually
docker build -t orbyt-flow:latest .
docker run -d -p 8085:8085 -v orbyt-data:/data orbyt-flow:latest
```

### Option 2: Build from source

```bash
# Build
go build -o orbyt-flow ./cmd/server

# Run HTTP server on :8085 (default)
./orbyt-flow

# Run MCP server for agent integration
MCP_MODE=true MCP_USER_ID=alice ./orbyt-flow
```

---

## Running as an HTTP Server

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8085` | TCP port the HTTP server listens on |
| `FLOWENGINE_DATA_DIR` | `./data` | Root directory for all persisted data |
| `MCP_MODE` | _(unset)_ | Set to `true` to switch to MCP mode |

### Data Directory Layout

orbyt-flow stores everything as JSON files — no database required.

```
{FLOWENGINE_DATA_DIR}/
├── index.json                          # Global workflow_id → user_id map (for webhooks)
└── {userID}/
    ├── workflows/
    │   └── {workflow_id}.json
    ├── runs/
    │   └── {run_id}.json
    ├── env.json                        # User-scoped environment variables (JSON)
    └── .env                            # User-scoped environment variables (dotenv)
```

Both `env.json` and `.env` are optional. Values in these files are available inside workflow node configs as `{{env.KEY_NAME}}`.

### REST API Reference

All endpoints except `GET /health` and `POST /webhook/{id}` require the `X-User-ID` header.

#### Health

```
GET /health
```

Response:
```json
{ "status": "ok", "version": "0.1.0" }
```

#### Workflows

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/workflows` | Create a new workflow |
| `GET` | `/workflows` | List all workflows for the user |
| `GET` | `/workflows/{id}` | Get a single workflow |
| `PUT` | `/workflows/{id}` | Replace a workflow (increments version) |
| `DELETE` | `/workflows/{id}` | Delete a workflow |

**Create workflow — request body:**

```json
{
  "name": "my-workflow",
  "trigger": { "type": "manual" },
  "nodes": [
    { "id": "fetch", "type": "http_request", "config": { "method": "GET", "url": "https://example.com/api" } },
    { "id": "log",   "type": "log",          "config": { "message": "{{fetch.body}}", "level": "info" } }
  ],
  "connections": [
    { "from": "fetch", "to": "log" }
  ],
  "error_handler": { "notify": "none", "retry": 0 }
}
```

**Create workflow — response:**

```json
{ "workflow_id": "3f2a...", "version": 1, "created_at": "2026-04-16T10:00:00Z" }
```

#### Execution

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/workflows/{id}/trigger` | Execute a workflow |
| `GET` | `/workflows/{id}/runs` | List runs for a workflow |
| `GET` | `/runs/{run_id}` | Get a single run result |

**Trigger request body:**

```json
{
  "mode": "sync",
  "payload": { "input_value": "hello" }
}
```

- `mode`: `sync` (waits for completion, returns full run) or `async` (returns immediately with `run_id`).
- `payload`: arbitrary JSON; accessible in node configs as `{{trigger.key}}`.

**Sync response** — full `Run` object:

```json
{
  "run_id": "ab12...",
  "workflow_id": "3f2a...",
  "status": "success",
  "steps": [
    { "node_id": "fetch", "status": "success", "output": { "status_code": 200, "body": "..." } },
    { "node_id": "log",   "status": "success", "output": { "logged": true } }
  ],
  "started_at": "2026-04-16T10:01:00Z",
  "finished_at": "2026-04-16T10:01:02Z"
}
```

**Async response:**

```json
{ "run_id": "ab12...", "status": "pending" }
```

Poll `GET /runs/{run_id}` to check completion.

#### Webhooks

```
POST /webhook/{workflow_id}
```

No `X-User-ID` header required. The request body becomes the trigger payload. Useful for receiving events from third-party services.

### Authentication

The HTTP server uses a simple per-request user identity model: include `X-User-ID: <your-user-id>` in every request. All workflows and runs are scoped to that ID — users cannot access each other's data.

```bash
curl -s -X POST http://localhost:8085/workflows \
  -H "X-User-ID: alice" \
  -H "Content-Type: application/json" \
  -d '{ "name": "test", "trigger": { "type": "manual" }, "nodes": [], "connections": [] }'
```

### User Environment Variables

Place a file at `{FLOWENGINE_DATA_DIR}/{userID}/env.json` to provide secrets and configuration to workflows without hard-coding them:

```json
{
  "ANTHROPIC_API_KEY": "sk-ant-...",
  "TELEGRAM_BOT_TOKEN": "123456:ABC...",
  "BASE_URL": "https://api.myservice.com"
}
```

Reference them in any node config field using `{{env.KEY_NAME}}`:

```json
{ "type": "llm_call", "config": { "api_key": "{{env.ANTHROPIC_API_KEY}}", "model": "claude-opus-4-6", "prompt": "Hello" } }
```

---

## Running as an MCP Server

Set `MCP_MODE=true` to start the engine as an MCP server over stdio. The server speaks the [Model Context Protocol](https://modelcontextprotocol.io) and exposes nine tools that any compatible client (Claude Desktop, Claude Code, custom SDK agents) can call.

### MCP Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_MODE` | _(unset)_ | Set to `true` to enable MCP mode |
| `MCP_USER_ID` | `default` | User identity for all operations in this session |
| `FLOWENGINE_DATA_DIR` | `./data` | Root directory for persisted data |

### MCP Tools Reference

| Tool | Required params | Description |
|------|----------------|-------------|
| `create_workflow` | `name` | Create a new workflow; returns `workflow_id` |
| `update_workflow` | `workflow_id` | Replace an existing workflow; increments version |
| `trigger_workflow` | `workflow_id` | Execute a workflow (`mode`: `sync`\|`async`) |
| `get_run_status` | `run_id` | Fetch per-step execution logs for a run |
| `list_workflows` | — | List all workflows owned by `MCP_USER_ID` |
| `delete_workflow` | `workflow_id`, `confirm: true` | Permanently delete a workflow |
| `list_user_secrets` | — | List env key names for `MCP_USER_ID` (values are never returned) |
| `upsert_user_secrets` | `secrets` (object map) | Merge keys into `{dataDir}/{userID}/env.json` for `{{env.KEY}}` in workflows |
| `delete_user_secret` | `key` | Remove one key from that user's `env.json` |

#### `create_workflow`

```json
{
  "name": "string (required)",
  "description": "string (optional)",
  "trigger": {
    "type": "manual | schedule | webhook",
    "cron": "0 9 * * 1-5",
    "tz": "America/New_York",
    "path": "/hooks/my-hook"
  },
  "nodes": [
    { "id": "node_id", "type": "node_type", "config": { ... } }
  ],
  "connections": [
    { "from": "node_a", "to": "node_b" }
  ],
  "error_handler": { "notify": "none | telegram | email", "retry": 2 }
}
```

#### `trigger_workflow`

```json
{
  "workflow_id": "3f2a...",
  "mode": "sync",
  "payload": { "any": "data" }
}
```

Returns the full `Run` object in sync mode. Returns `{ "run_id": "...", "status": "pending" }` in async mode.

---

## Connecting to a Claude Agent

### Claude Desktop

Add orbyt-flow as an MCP server in `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS):

```json
{
  "mcpServers": {
    "orbyt-flow": {
      "command": "/path/to/orbyt-flow",
      "env": {
        "MCP_MODE": "true",
        "MCP_USER_ID": "alice",
        "FLOWENGINE_DATA_DIR": "/Users/alice/.orbyt-flow/data"
      }
    }
  }
}
```

Restart Claude Desktop. The orbyt-flow tools (workflows + user secrets) will appear in the tool picker. You can now ask Claude:

> "Create a workflow that fetches the latest news from https://news-api.example.com every morning at 9 AM and sends it to my Telegram."

Claude will call `create_workflow` with the correct node graph and connections.

### Claude Code CLI

Add the server to your Claude Code settings at `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "orbyt-flow": {
      "command": "/path/to/orbyt-flow",
      "env": {
        "MCP_MODE": "true",
        "MCP_USER_ID": "alice",
        "FLOWENGINE_DATA_DIR": "/Users/alice/.orbyt-flow/data"
      }
    }
  }
}
```

Or use the project-level config at `.claude/settings.json` in your repo root to scope it to one project.

After restarting Claude Code, the tools are available in any conversation. Example prompt:

```
/mcp orbyt-flow list_workflows
```

Or just describe what you want:

> "List all my workflows and trigger the one named 'daily-report'."

### Custom Agent via the Anthropic SDK

Use orbyt-flow as a subprocess MCP server in your own agent. The server communicates over stdin/stdout.

**Node.js example (TypeScript):**

```typescript
import Anthropic from "@anthropic-ai/sdk";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

// 1. Spin up orbyt-flow in MCP mode as a child process
const transport = new StdioClientTransport({
  command: "/path/to/orbyt-flow",
  env: {
    ...process.env,
    MCP_MODE: "true",
    MCP_USER_ID: "agent-user",
    FLOWENGINE_DATA_DIR: "./data",
  },
});

const mcp = new Client({ name: "my-agent", version: "1.0.0" }, { capabilities: {} });
await mcp.connect(transport);

// 2. Discover available tools
const { tools } = await mcp.listTools();
const anthropicTools = tools.map((t) => ({
  name: t.name,
  description: t.description ?? "",
  input_schema: t.inputSchema,
}));

// 3. Run the agent loop
const client = new Anthropic();
const messages: Anthropic.MessageParam[] = [
  { role: "user", content: "Create a workflow that logs 'hello world' and trigger it." },
];

while (true) {
  const response = await client.messages.create({
    model: "claude-opus-4-6",
    max_tokens: 4096,
    tools: anthropicTools,
    messages,
  });

  messages.push({ role: "assistant", content: response.content });

  if (response.stop_reason === "end_turn") break;

  // Execute tool calls
  const toolResults: Anthropic.ToolResultBlockParam[] = [];
  for (const block of response.content) {
    if (block.type !== "tool_use") continue;

    const result = await mcp.callTool({ name: block.name, arguments: block.input as Record<string, unknown> });
    toolResults.push({
      type: "tool_result",
      tool_use_id: block.id,
      content: result.content as string,
    });
  }

  messages.push({ role: "user", content: toolResults });
}

await mcp.close();
```

**Python example:**

```python
import asyncio
import anthropic
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

async def run_agent():
    server_params = StdioServerParameters(
        command="/path/to/orbyt-flow",
        env={
            "MCP_MODE": "true",
            "MCP_USER_ID": "agent-user",
            "FLOWENGINE_DATA_DIR": "./data",
        },
    )

    async with stdio_client(server_params) as (read, write):
        async with ClientSession(read, write) as session:
            await session.initialize()

            # Discover tools
            tools_response = await session.list_tools()
            tools = [
                {
                    "name": t.name,
                    "description": t.description or "",
                    "input_schema": t.inputSchema,
                }
                for t in tools_response.tools
            ]

            client = anthropic.Anthropic()
            messages = [
                {"role": "user", "content": "List all my workflows."}
            ]

            while True:
                response = client.messages.create(
                    model="claude-opus-4-6",
                    max_tokens=4096,
                    tools=tools,
                    messages=messages,
                )

                messages.append({"role": "assistant", "content": response.content})

                if response.stop_reason == "end_turn":
                    print(response.content[0].text)
                    break

                tool_results = []
                for block in response.content:
                    if block.type != "tool_use":
                        continue
                    result = await session.call_tool(block.name, arguments=block.input)
                    tool_results.append({
                        "type": "tool_result",
                        "tool_use_id": block.id,
                        "content": str(result.content),
                    })

                messages.append({"role": "user", "content": tool_results})

asyncio.run(run_agent())
```

---

## Workflow Schema

### Node Types

Every node has three fields: `id` (unique string), `type`, and `config` (type-specific JSON object).

#### `http_request`

Performs an HTTP/HTTPS request.

```json
{
  "id": "fetch_data",
  "type": "http_request",
  "config": {
    "method": "POST",
    "url": "https://api.example.com/data",
    "headers": { "Content-Type": "application/json" },
    "body": "{\"key\": \"{{vars.my_key}}\"}",
    "auth": { "type": "bearer", "token": "{{env.API_TOKEN}}" },
    "timeout_seconds": 10
  }
}
```

Auth types: `bearer` (`token`), `basic` (`username`, `password`).

Output: `{ "status_code": 200, "body": "...", "headers": { ... } }`

#### `llm_call`

Calls the Anthropic Claude API.

```json
{
  "id": "summarize",
  "type": "llm_call",
  "config": {
    "model": "claude-opus-4-6",
    "prompt": "Summarize: {{fetch_data.body}}",
    "system": "You are a helpful assistant.",
    "max_tokens": 1024,
    "api_key": "{{env.ANTHROPIC_API_KEY}}"
  }
}
```

Output: `{ "text": "...", "input_tokens": 42, "output_tokens": 100 }`

#### `if`

Conditional branch. Routes execution to one of two paths.

```json
{
  "id": "check_status",
  "type": "if",
  "config": {
    "condition": "{{fetch_data.status_code}} == 200",
    "true_next": "process_ok",
    "false_next": "handle_error"
  }
}
```

Supported operators: `==`, `!=`, `>`, `<`, `>=`, `<=`

Output: `{ "result": true, "next": "process_ok" }`

Nodes that become unreachable after a branch are automatically marked `skipped`.

#### `set_variable`

Stores a value in the workflow's variable context for later use as `{{vars.KEY}}`.

```json
{
  "id": "store_count",
  "type": "set_variable",
  "config": { "key": "total", "value": "{{fetch_data.body.count}}" }
}
```

#### `log`

Emits a message at the specified log level.

```json
{
  "id": "debug_log",
  "type": "log",
  "config": { "message": "Got {{fetch_data.status_code}}", "level": "info" }
}
```

Levels: `info`, `warn`, `error`

#### `wait`

Pauses execution for a fixed number of seconds.

```json
{
  "id": "pause",
  "type": "wait",
  "config": { "seconds": 5 }
}
```

#### `stop`

Terminates the workflow immediately. Useful for early exits.

```json
{
  "id": "abort",
  "type": "stop",
  "config": { "message": "Unexpected state", "is_error": true }
}
```

Set `is_error: false` for a successful early exit.

#### `send_telegram`

Sends a message via a Telegram bot.

```json
{
  "id": "notify",
  "type": "send_telegram",
  "config": {
    "bot_token": "{{env.TELEGRAM_BOT_TOKEN}}",
    "chat_id": "{{env.TELEGRAM_CHAT_ID}}",
    "message": "Run complete: {{summarize.text}}"
  }
}
```

Output: `{ "sent": true, "message_id": 42 }`

---

### Template Variables

Use `{{namespace.path}}` in any string value inside a node's `config` object.

| Syntax | Resolves to |
|--------|-------------|
| `{{env.KEY}}` | Value from `env.json` or `.env` for the current user |
| `{{vars.KEY}}` | Value set by a preceding `set_variable` node |
| `{{nodeID.field}}` | Top-level output field of a previous node |
| `{{nodeID.nested.path}}` | Nested field (dot notation) |
| `{{nodeID.array.0}}` | First element of an array field (numeric segment after the array) |
| `{{nodeID.path[i].key}}` | Array index `i`; use `[-1]` for the last element |
| `{{nodeID.path[*].key}}` | `key` from every array element, joined with newlines |
| `{{trigger.field}}` | Field from the trigger payload passed to `/trigger` |

**Default:** append ` | default: "fallback"` (double-quoted) to any expression; if the path is missing, the fallback string is used instead of an error.

In JSON configs, string fields stay strings. If a template resolves to an object or array, it is embedded as JSON text (safe for `"body": "{{node.output}}"` in HTTP nodes). Mixing literal text and templates still JSON-encodes object/array fragments inside the string.

Example combining multiple sources:

```json
{
  "url": "{{env.BASE_URL}}/users/{{trigger.user_id}}",
  "body": "{\"note\": \"{{prev_node.result.items.0.name}}\"}"
}
```

---

### Connections & Branching

`connections` is an array of `{ "from": "node_a", "to": "node_b" }` pairs. The executor builds a DAG and runs nodes in topological order.

For linear chains:
```json
"connections": [
  { "from": "step1", "to": "step2" },
  { "from": "step2", "to": "step3" }
]
```

For `if` branching, define connections from the if-node to both branches, then reconverge if needed:

```json
"connections": [
  { "from": "check",      "to": "handle_ok"    },
  { "from": "check",      "to": "handle_err"   },
  { "from": "handle_ok",  "to": "final_step"   },
  { "from": "handle_err", "to": "final_step"   }
]
```

The `if` node's `true_next` / `false_next` config fields control which branch is taken at runtime; the non-taken branch is marked `skipped`.

Cycles are detected at execution time via Kahn's algorithm — a workflow with a cycle will fail immediately with a descriptive error.

---

### Error Handling & Retries

Set `error_handler` at the workflow level:

```json
"error_handler": {
  "notify": "telegram",
  "retry": 3
}
```

- `retry`: number of additional attempts after the first failure. A value of `3` means up to 4 total attempts. Each retry waits 2 seconds.
- `notify`: `telegram`, `email`, or `none`. (Telegram notification is active; email is planned.)

Each node execution has a hard timeout of 30 seconds regardless of `retry`.

---

## End-to-End Examples

### HTTP API: Create and trigger a workflow

```bash
# 1. Start the server
./orbyt-flow

# 2. Create a workflow that fetches a URL and logs the status
curl -s -X POST http://localhost:8085/workflows \
  -H "X-User-ID: alice" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "health-check",
    "trigger": { "type": "manual" },
    "nodes": [
      {
        "id": "ping",
        "type": "http_request",
        "config": { "method": "GET", "url": "https://httpbin.org/get", "timeout_seconds": 5 }
      },
      {
        "id": "report",
        "type": "log",
        "config": { "message": "Status: {{ping.status_code}}", "level": "info" }
      }
    ],
    "connections": [{ "from": "ping", "to": "report" }],
    "error_handler": { "notify": "none", "retry": 1 }
  }'
# → { "workflow_id": "3f2a...", "version": 1 }

# 3. Trigger it synchronously
curl -s -X POST http://localhost:8085/workflows/3f2a.../trigger \
  -H "X-User-ID: alice" \
  -H "Content-Type: application/json" \
  -d '{ "mode": "sync" }'
# → { "run_id": "ab12...", "status": "success", "steps": [...] }
```

### MCP: Agent-driven workflow creation

```bash
# Start in MCP mode
MCP_MODE=true MCP_USER_ID=alice FLOWENGINE_DATA_DIR=./data ./orbyt-flow
```

With Claude Desktop configured (see above), ask Claude:

> "Create a workflow called 'llm-summarizer' that takes a URL from the trigger payload, fetches the page, passes the body to Claude with a summarize prompt, and logs the result. Then trigger it with url='https://example.com'."

Claude will call:
1. `create_workflow` — builds the node graph with `http_request`, `llm_call`, and `log` nodes
2. `trigger_workflow` — runs it with `{ "url": "https://example.com" }` as the payload
3. `get_run_status` — (if async) polls until complete and returns the summary

---

## Docker Deployment

### Using Docker Compose (recommended)

```bash
docker compose up -d
```

This starts orbyt-flow on port 8085 with persistent data stored in a Docker volume.

### Using Docker directly

```bash
# Build the image
docker build -t orbyt-flow:latest .

# Run with a named volume for data persistence
docker run -d \
  --name orbyt-flow \
  -p 8085:8085 \
  -v orbyt-data:/data \
  orbyt-flow:latest

# Or bind-mount a host directory
docker run -d \
  --name orbyt-flow \
  -p 8085:8085 \
  -v /path/on/host:/data \
  orbyt-flow:latest
```

### Environment variables

Pass environment variables with `-e`:

```bash
docker run -d \
  -p 9000:9000 \
  -v orbyt-data:/data \
  -e PORT=9000 \
  -e FLOWENGINE_DATA_DIR=/data \
  orbyt-flow:latest
```

### Health check

The container includes a health check that polls `/health`. Check status with:

```bash
docker inspect --format='{{.State.Health.Status}}' orbyt-flow
```

---

## Development

```bash
# Run tests
go test ./...

# Run with live reload (requires Air)
air

# Build binary
go build -o orbyt-flow ./cmd/server

# Run HTTP server locally
FLOWENGINE_DATA_DIR=./data ./orbyt-flow

# Run MCP server locally (for testing with an MCP inspector)
MCP_MODE=true MCP_USER_ID=dev FLOWENGINE_DATA_DIR=./data ./orbyt-flow
```

### Adding a new node type

1. Create `internal/runner/my_node.go` implementing the `runner.Runner` interface:
   ```go
   func (r *MyNodeRunner) Run(ctx context.Context, input runner.Input) (*runner.Output, error) { ... }
   ```
2. Register it in `internal/runner/runner.go`:
   ```go
   "my_node": &MyNodeRunner{},
   ```
3. Add the config struct to `internal/types/node.go`.
4. Write tests in `internal/runner/my_node_test.go`.

### Project layout

```
cmd/server/         # Entry point
internal/
  api/              # HTTP REST server
  mcp/              # MCP stdio server
  executor/         # DAG execution engine
  runner/           # Per-node-type runners
  store/            # File-based persistence
  template/         # {{...}} variable resolution
  types/            # Shared data types
  env/              # User env loading
```
