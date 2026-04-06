# Nexus Gateway — Node.js Quickstart

**Nexus is a drop-in proxy — your existing OpenAI code works, just change the URL.**

No new SDK to learn. Use the official `openai` npm package you already have.

---

## 3-Step Quickstart

### 1. Install

```bash
npm install openai
```

### 2. Configure — point at Nexus

```javascript
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "https://nexus-gateway.example.com/v1",   // ← only change
  apiKey: process.env.NEXUS_API_KEY ?? "your-api-key",
});
```

### 3. Send your first request

```javascript
const response = await client.chat.completions.create({
  model: "auto",                                       // Nexus picks the best model
  messages: [{ role: "user", content: "Hello!" }],
});
console.log(response.choices[0].message.content);
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

Pass these via the second argument's `headers` option:

```javascript
const response = await client.chat.completions.create(
  {
    model: "auto",
    messages: [{ role: "user", content: "Design a rate limiter." }],
  },
  {
    headers: {
      "X-Workflow-ID": "design-session-42",
      "X-Agent-Role":  "architect",
      "X-Team":        "platform-eng",
      "X-Budget":      "2.50",
    },
  }
);
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

Read them with `.withResponse()`:

```javascript
const { data: completion, response } =
  await client.chat.completions
    .create({
      model: "auto",
      messages: [{ role: "user", content: "Hello!" }],
    })
    .withResponse();

console.log(response.headers.get("x-nexus-tier"));       // "mid"
console.log(response.headers.get("x-nexus-cost"));       // "0.001500"
console.log(response.headers.get("x-nexus-provider"));   // "openai"
console.log(completion.choices[0].message.content);
```

---

## Streaming

```javascript
const stream = await client.chat.completions.create({
  model: "auto",
  messages: [{ role: "user", content: "Write a haiku." }],
  stream: true,
});

for await (const chunk of stream) {
  const delta = chunk.choices[0]?.delta?.content;
  if (delta) process.stdout.write(delta);
}
```

---

## Multi-Step Workflow

```javascript
const workflowId = "code-review-session";
const steps = [
  ["researcher", "Find best practices for Go error handling."],
  ["architect",  "Design an error handling strategy for our API."],
  ["tester",     "Write table-driven tests for the error middleware."],
];

for (let i = 0; i < steps.length; i++) {
  const [role, prompt] = steps[i];

  const { data: completion, response } =
    await client.chat.completions
      .create(
        {
          model: "auto",
          messages: [{ role: "user", content: prompt }],
        },
        {
          headers: {
            "X-Workflow-ID":  workflowId,
            "X-Agent-Role":   role,
            "X-Step-Number":  String(i + 1),
            "X-Budget":       "5.00",
          },
        }
      )
      .withResponse();

  console.log(
    `Step ${i + 1} [${role}]: tier=${response.headers.get("x-nexus-tier")}, ` +
    `cost=$${response.headers.get("x-nexus-cost")}`
  );
}
```

---

## Full Example

See [`example.js`](example.js) for a complete runnable example covering non-streaming, streaming, Nexus headers, and multi-step workflows.

## Docs

Full documentation: [nexus-gateway/nexus](https://github.com/nexus-gateway/nexus)

## License

Apache-2.0
