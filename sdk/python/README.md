# Nexus Gateway — Python Quickstart

**Nexus is a drop-in proxy — your existing OpenAI code works, just change the URL.**

No new SDK to learn. Use the official `openai` package you already have.

---

## 3-Step Quickstart

### 1. Install

```bash
pip install openai
```

### 2. Configure — point at Nexus

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://nexus-gateway.example.com/v1",   # ← only change
    api_key="your-api-key",
)
```

### 3. Send your first request

```python
response = client.chat.completions.create(
    model="auto",                                       # Nexus picks the best model
    messages=[{"role": "user", "content": "Hello!"}],
)
print(response.choices[0].message.content)
```

That's it. Every feature of the OpenAI SDK — streaming, tools, JSON mode — works unchanged.

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

Pass these via `extra_headers`:

```python
response = client.chat.completions.create(
    model="auto",
    messages=[{"role": "user", "content": "Design a rate limiter."}],
    extra_headers={
        "X-Workflow-ID": "design-session-42",
        "X-Agent-Role":  "architect",
        "X-Team":        "platform-eng",
        "X-Budget":      "2.50",
    },
)
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

Read them with `with_raw_response`:

```python
raw = client.chat.completions.with_raw_response.create(
    model="auto",
    messages=[{"role": "user", "content": "Hello!"}],
)

completion = raw.parse()
print(raw.headers.get("x-nexus-tier"))       # "mid"
print(raw.headers.get("x-nexus-cost"))       # "0.001500"
print(raw.headers.get("x-nexus-provider"))   # "openai"
print(completion.choices[0].message.content)
```

---

## Streaming

```python
stream = client.chat.completions.create(
    model="auto",
    messages=[{"role": "user", "content": "Write a haiku."}],
    stream=True,
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

---

## Multi-Step Workflow

```python
workflow_id = "code-review-session"

for i, (role, prompt) in enumerate([
    ("researcher", "Find best practices for Go error handling."),
    ("architect",  "Design an error handling strategy for our API."),
    ("tester",     "Write table-driven tests for the error middleware."),
], start=1):
    raw = client.chat.completions.with_raw_response.create(
        model="auto",
        messages=[{"role": "user", "content": prompt}],
        extra_headers={
            "X-Workflow-ID":  workflow_id,
            "X-Agent-Role":   role,
            "X-Step-Number":  str(i),
            "X-Budget":       "5.00",
        },
    )
    result = raw.parse()
    print(f"Step {i} [{role}]: tier={raw.headers.get('x-nexus-tier')}, "
          f"cost=${raw.headers.get('x-nexus-cost')}")
```

---

## Nexus-Native Python SDK

For deeper integration (automatic workflow tracking, budget management, LangChain/CrewAI support), see the Nexus-native client:

```bash
pip install nexus-gateway                    # Core SDK
pip install nexus-gateway[langchain]         # + LangChain integration
pip install nexus-gateway[crewai]            # + CrewAI integration
```

```python
from nexus_gateway import NexusClient

client = NexusClient("https://nexus-gateway.example.com")

with client.workflow(budget=1.0) as wf:
    plan = wf.chat([{"role": "user", "content": "Plan a project"}])
    code = wf.chat([{"role": "user", "content": f"Implement: {plan.content}"}])
    wf.feedback(rating=0.9)
    print(wf.summary)
```

---

## Full Example

See [`example.py`](example.py) for a complete runnable example covering non-streaming, streaming, Nexus headers, and multi-step workflows.

## Docs

Full documentation: [nexus-gateway/nexus](https://github.com/nexus-gateway/nexus)

## License

Apache-2.0
