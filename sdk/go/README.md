# Nexus Gateway — Go Quickstart

**Nexus is a drop-in proxy — any OpenAI-compatible HTTP client works, just change the URL.**

No external dependencies required. Use Go's standard `net/http` + `encoding/json`.

---

## 3-Step Quickstart

### 1. Install

Nothing to install — only the Go standard library is needed.

```bash
# No external dependencies
```

### 2. Configure — point at Nexus

```go
const nexusURL = "https://nexus-gateway.example.com/v1/chat/completions"
apiKey := os.Getenv("NEXUS_API_KEY")
```

### 3. Send your first request

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
)

func main() {
    body, _ := json.Marshal(map[string]interface{}{
        "model": "auto",
        "messages": []map[string]string{
            {"role": "user", "content": "Hello!"},
        },
    })

    req, _ := http.NewRequest("POST",
        "https://nexus-gateway.example.com/v1/chat/completions",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+os.Getenv("NEXUS_API_KEY"))

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    data, _ := io.ReadAll(resp.Body)
    fmt.Println(string(data))
}
```

That's it. Any HTTP client that can POST JSON to an OpenAI-compatible endpoint works with Nexus.

---

## Nexus-Specific Headers

Add optional headers to unlock Nexus routing, workflow tracking, and cost management.

### Request Headers (you send)

| Header | Description | Example |
|---|---|---|
| `X-Workflow-ID` | Groups related requests into a workflow for cumulative cost tracking | `"design-session-42"` |
| `X-Agent-Role` | Hints the router about task type — affects model/tier selection | `"architect"`, `"researcher"`, `"chat"`, `"tester"` |
| `X-Team` | Team identifier for billing and cost attribution | `"platform-eng"` |
| `X-Budget` | Maximum USD budget for this workflow | `"2.50"` |
| `X-Step-Number` | Position in a multi-step workflow | `"3"` |
| `X-Request-ID` | Trace ID for request correlation (auto-generated if omitted) | `"req-abc123"` |

```go
req.Header.Set("X-Workflow-ID", "design-session-42")
req.Header.Set("X-Agent-Role", "architect")
req.Header.Set("X-Team", "platform-eng")
req.Header.Set("X-Budget", "2.50")
```

### Response Headers (Nexus returns)

| Header | Description | Example |
|---|---|---|
| `X-Nexus-Model` | Model Nexus actually used | `"gpt-4o"` |
| `X-Nexus-Tier` | Routing tier selected | `"cheap"`, `"mid"`, `"premium"` |
| `X-Nexus-Provider` | Backend provider used | `"openai"`, `"anthropic"`, `"cache/L1"` |
| `X-Nexus-Cost` | Estimated cost in USD | `"0.003200"` |
| `X-Nexus-Cache` | Cache layer if served from cache | `"L1"`, `"L2a"`, `"L2b"` |
| `X-Nexus-Confidence` | Response quality score (0–1) | `"0.850"` |
| `X-Nexus-Workflow-ID` | Echoed workflow ID | `"design-session-42"` |
| `X-Nexus-Workflow-Step` | Current step in workflow | `"2"` |

```go
resp, _ := http.DefaultClient.Do(req)
fmt.Println("Model:", resp.Header.Get("X-Nexus-Model"))     // "gpt-4o"
fmt.Println("Tier:",  resp.Header.Get("X-Nexus-Tier"))      // "mid"
fmt.Println("Cost:",  resp.Header.Get("X-Nexus-Cost"))      // "0.001500"
fmt.Println("Cache:", resp.Header.Get("X-Nexus-Cache"))      // "L1" or ""
```

---

## Streaming

Parse Server-Sent Events line by line:

```go
req.Header.Set("Content-Type", "application/json")
// body includes "stream": true

resp, _ := client.Do(req)
defer resp.Body.Close()

scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    line := scanner.Text()
    if !strings.HasPrefix(line, "data: ") {
        continue
    }
    payload := strings.TrimPrefix(line, "data: ")
    if payload == "[DONE]" {
        break
    }
    // Parse JSON chunk and extract delta.content
}
```

---

## Multi-Step Workflow

```go
steps := []struct{ role, prompt string }{
    {"researcher", "Find best practices for Go error handling."},
    {"architect",  "Design an error handling strategy for our API."},
    {"tester",     "Write table-driven tests for the error middleware."},
}

for i, s := range steps {
    req.Header.Set("X-Workflow-ID", "code-review-session")
    req.Header.Set("X-Agent-Role", s.role)
    req.Header.Set("X-Step-Number", fmt.Sprintf("%d", i+1))
    req.Header.Set("X-Budget", "5.00")

    resp, _ := client.Do(req)
    fmt.Printf("Step %d [%s]: tier=%s, cost=$%s\n",
        i+1, s.role,
        resp.Header.Get("X-Nexus-Tier"),
        resp.Header.Get("X-Nexus-Cost"))
    resp.Body.Close()
}
```

---

## Full Example

See [`example.go`](example.go) for a complete runnable example covering non-streaming, streaming, Nexus headers, multi-step workflows, and health checks.

## Docs

Full documentation: [nexus-gateway/nexus](https://github.com/nexus-gateway/nexus)

## License

BSL 1.1 — see [LICENSE](../../LICENSE)
