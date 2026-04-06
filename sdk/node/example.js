/**
 * Nexus Gateway — OpenAI SDK Drop-in Example (Node.js)
 *
 * Nexus is fully OpenAI-compatible. Use the official `openai` npm package —
 * just change the baseURL and optionally add Nexus routing headers.
 *
 *     npm install openai
 */

import OpenAI from "openai";

// ── 1. Configure the client ─────────────────────────────────────────────
//
// Point the standard OpenAI SDK at your Nexus gateway.
// Everything else (models, messages, tools) works exactly the same.

const client = new OpenAI({
  baseURL: "https://nexus-gateway.example.com/v1", // ← only change
  apiKey: process.env.NEXUS_API_KEY ?? "your-api-key",
});

// ── 2. Non-streaming chat completion ────────────────────────────────────

async function basicCompletion() {
  const response = await client.chat.completions.create({
    model: "auto", // let Nexus router pick the best model
    messages: [
      { role: "system", content: "You are a helpful assistant." },
      {
        role: "user",
        content: "Explain the CAP theorem in two sentences.",
      },
    ],
    temperature: 0.7,
    max_tokens: 256,
  });

  console.log("=== Non-Streaming Response ===");
  console.log(`Content : ${response.choices[0].message.content}`);
  console.log(`Model   : ${response.model}`);
  console.log(
    `Tokens  : ${response.usage.prompt_tokens}+${response.usage.completion_tokens}`
  );
}

// ── 3. Streaming chat completion ────────────────────────────────────────

async function streamingCompletion() {
  console.log("\n=== Streaming Response ===");

  const stream = await client.chat.completions.create({
    model: "auto",
    messages: [
      {
        role: "user",
        content: "Write a haiku about distributed systems.",
      },
    ],
    stream: true,
  });

  for await (const chunk of stream) {
    const delta = chunk.choices[0]?.delta?.content;
    if (delta) process.stdout.write(delta);
  }
  console.log(); // newline after stream
}

// ── 4. Using Nexus-specific headers ─────────────────────────────────────
//
// Nexus adds optional request headers for workflow tracking, agent routing,
// and cost management.  Pass them via the `headers` option on each call.
//
//   Request headers:
//     X-Workflow-ID   — Group related requests into a workflow for cost tracking.
//     X-Agent-Role    — Hint the router about the task type ("architect",
//                       "researcher", "chat", "tester"). Affects model selection.
//     X-Team          — Team identifier for billing / cost attribution.
//     X-Budget        — Maximum USD budget for this workflow.
//     X-Request-ID    — Optional trace ID (auto-generated if omitted).

async function nexusRoutedCompletion() {
  const response = await client.chat.completions.create(
    {
      model: "auto",
      messages: [
        { role: "system", content: "You are a senior software architect." },
        {
          role: "user",
          content: "Design a rate-limiting service for 10M RPM.",
        },
      ],
      max_tokens: 1024,
    },
    {
      headers: {
        "X-Workflow-ID": "design-session-42",
        "X-Agent-Role": "architect",
        "X-Team": "platform-eng",
        "X-Budget": "2.50",
      },
    }
  );

  console.log("\n=== Nexus-Routed Response ===");
  console.log(
    `Content : ${response.choices[0].message.content.slice(0, 120)}...`
  );
  console.log(`Model   : ${response.model}`);
}

// ── 5. Reading Nexus response headers ───────────────────────────────────
//
// Nexus returns routing metadata in response headers.
// Use `.withResponse()` to access both the parsed body and raw headers.
//
//   Response headers:
//     X-Nexus-Model         — The model Nexus actually used (e.g. "gpt-4o").
//     X-Nexus-Tier          — Routing tier: "cheap", "mid", or "premium".
//     X-Nexus-Provider      — Backend provider: "openai", "anthropic", "cache/L1".
//     X-Nexus-Cost          — Estimated cost in USD (e.g. "0.003200").
//     X-Nexus-Cache         — Cache layer if served from cache ("L1", "L2a", "L2b").
//     X-Nexus-Confidence    — Response quality score (0–1).
//     X-Nexus-Workflow-ID   — Echoed workflow ID.
//     X-Nexus-Workflow-Step — Current step number in the workflow.

async function readNexusHeaders() {
  const { data: completion, response } =
    await client.chat.completions
      .create(
        {
          model: "auto",
          messages: [{ role: "user", content: "What is 2 + 2?" }],
        },
        {
          headers: {
            "X-Workflow-ID": "demo-workflow-1",
            "X-Agent-Role": "chat",
          },
        }
      )
      .withResponse();

  console.log("\n=== Nexus Response Headers ===");
  console.log(`Model      : ${response.headers.get("x-nexus-model") ?? "n/a"}`);
  console.log(`Tier       : ${response.headers.get("x-nexus-tier") ?? "n/a"}`);
  console.log(
    `Provider   : ${response.headers.get("x-nexus-provider") ?? "n/a"}`
  );
  console.log(
    `Cost (USD) : ${response.headers.get("x-nexus-cost") ?? "n/a"}`
  );
  console.log(
    `Cache      : ${response.headers.get("x-nexus-cache") ?? "miss"}`
  );
  console.log(
    `Confidence : ${response.headers.get("x-nexus-confidence") ?? "n/a"}`
  );
  console.log(
    `Workflow   : ${response.headers.get("x-nexus-workflow-id") ?? "n/a"}`
  );
  console.log(
    `Step       : ${response.headers.get("x-nexus-workflow-step") ?? "n/a"}`
  );
  console.log(`Answer     : ${completion.choices[0].message.content}`);
}

// ── 6. Multi-step workflow with header inspection ───────────────────────

async function workflowExample() {
  const workflowId = "onboarding-flow-99";
  const steps = [
    ["researcher", "List the top 3 Python web frameworks and their strengths."],
    ["architect", "Given those frameworks, which is best for a high-traffic API?"],
    ["tester", "Write a pytest test for a FastAPI health endpoint."],
  ];

  console.log("\n=== Multi-Step Workflow ===");

  for (let i = 0; i < steps.length; i++) {
    const [role, prompt] = steps[i];

    const { data: completion, response } =
      await client.chat.completions
        .create(
          {
            model: "auto",
            messages: [{ role: "user", content: prompt }],
            max_tokens: 512,
          },
          {
            headers: {
              "X-Workflow-ID": workflowId,
              "X-Agent-Role": role,
              "X-Step-Number": String(i + 1),
              "X-Budget": "5.00",
            },
          }
        )
        .withResponse();

    const tier = response.headers.get("x-nexus-tier") ?? "?";
    const cost = response.headers.get("x-nexus-cost") ?? "?";
    const model = response.headers.get("x-nexus-model") ?? "?";
    const preview = completion.choices[0].message.content
      .slice(0, 80)
      .replace(/\n/g, " ");

    console.log(
      `  Step ${i + 1} [${role.padStart(12)}] → tier=${tier}, model=${model}, cost=$${cost}`
    );
    console.log(`    ${preview}...`);
  }
}

// ── Run all examples ────────────────────────────────────────────────────

async function main() {
  await basicCompletion();
  await streamingCompletion();
  await nexusRoutedCompletion();
  await readNexusHeaders();
  await workflowExample();
}

main().catch(console.error);
