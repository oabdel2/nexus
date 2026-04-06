"""
Nexus Gateway — OpenAI SDK Drop-in Example (Python)

Nexus is fully OpenAI-compatible. Use the official `openai` package — just
change the base_url and optionally add Nexus routing headers.

    pip install openai

"""

import os
from openai import OpenAI

# ── 1. Configure the client ─────────────────────────────────────────────
#
# Point the standard OpenAI SDK at your Nexus gateway.
# Everything else (models, messages, tools) works exactly the same.

client = OpenAI(
    base_url="https://nexus-gateway.example.com/v1",   # ← only change
    api_key=os.getenv("NEXUS_API_KEY", "your-api-key"),
)


# ── 2. Non-streaming chat completion ────────────────────────────────────

def basic_completion():
    """Standard chat completion — identical to OpenAI, routed through Nexus."""
    response = client.chat.completions.create(
        model="auto",  # let Nexus router pick the best model
        messages=[
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "Explain the CAP theorem in two sentences."},
        ],
        temperature=0.7,
        max_tokens=256,
    )

    print("=== Non-Streaming Response ===")
    print(f"Content : {response.choices[0].message.content}")
    print(f"Model   : {response.model}")
    print(f"Tokens  : {response.usage.prompt_tokens}+{response.usage.completion_tokens}")


# ── 3. Streaming chat completion ────────────────────────────────────────

def streaming_completion():
    """Server-sent events streaming — works identically to OpenAI streaming."""
    print("\n=== Streaming Response ===")

    stream = client.chat.completions.create(
        model="auto",
        messages=[
            {"role": "user", "content": "Write a haiku about distributed systems."},
        ],
        stream=True,
    )

    for chunk in stream:
        delta = chunk.choices[0].delta
        if delta.content:
            print(delta.content, end="", flush=True)

    print()  # newline after stream


# ── 4. Using Nexus-specific headers ────────────────────────────────────
#
# Nexus adds optional request headers for workflow tracking, agent routing,
# and cost management.  Pass them via `extra_headers`.

def nexus_routed_completion():
    """
    Send Nexus routing headers to control tier selection and workflow tracking.

    Request headers:
        X-Workflow-ID   — Groups related requests into a workflow for cost tracking.
        X-Agent-Role    — Hints the router about the task type (e.g. "architect",
                          "researcher", "chat", "tester"). Affects model selection.
        X-Team          — Team identifier for billing / cost attribution.
        X-Budget        — Maximum USD budget for this workflow.
        X-Request-ID    — Optional trace ID (auto-generated if omitted).
    """
    response = client.chat.completions.create(
        model="auto",
        messages=[
            {"role": "system", "content": "You are a senior software architect."},
            {"role": "user", "content": "Design a rate-limiting service for 10M RPM."},
        ],
        max_tokens=1024,
        extra_headers={
            "X-Workflow-ID": "design-session-42",
            "X-Agent-Role": "architect",
            "X-Team": "platform-eng",
            "X-Budget": "2.50",
        },
    )

    print("\n=== Nexus-Routed Response ===")
    print(f"Content : {response.choices[0].message.content[:120]}...")
    print(f"Model   : {response.model}")


# ── 5. Reading Nexus response headers ──────────────────────────────────
#
# Nexus returns metadata about routing decisions in response headers.
# Use `with_raw_response` to access them.

def read_nexus_headers():
    """
    Response headers returned by Nexus:
        X-Nexus-Model       — The model Nexus actually used (e.g. "gpt-4o").
        X-Nexus-Tier        — Routing tier: "cheap", "mid", or "premium".
        X-Nexus-Provider    — Backend provider: "openai", "anthropic", "cache/L1", etc.
        X-Nexus-Cost        — Estimated cost in USD (e.g. "0.003200").
        X-Nexus-Cache       — Cache layer if served from cache ("L1", "L2a", "L2b").
        X-Nexus-Confidence  — Response quality score from the eval engine (0-1).
        X-Nexus-Workflow-ID — Echoed workflow ID.
        X-Nexus-Workflow-Step — Current step number in the workflow.
    """
    raw = client.chat.completions.with_raw_response.create(
        model="auto",
        messages=[{"role": "user", "content": "What is 2 + 2?"}],
        extra_headers={
            "X-Workflow-ID": "demo-workflow-1",
            "X-Agent-Role": "chat",
        },
    )

    # Parse the completion as usual
    completion = raw.parse()

    print("\n=== Nexus Response Headers ===")
    print(f"Model      : {raw.headers.get('x-nexus-model', 'n/a')}")
    print(f"Tier       : {raw.headers.get('x-nexus-tier', 'n/a')}")
    print(f"Provider   : {raw.headers.get('x-nexus-provider', 'n/a')}")
    print(f"Cost (USD) : {raw.headers.get('x-nexus-cost', 'n/a')}")
    print(f"Cache      : {raw.headers.get('x-nexus-cache', 'miss')}")
    print(f"Confidence : {raw.headers.get('x-nexus-confidence', 'n/a')}")
    print(f"Workflow   : {raw.headers.get('x-nexus-workflow-id', 'n/a')}")
    print(f"Step       : {raw.headers.get('x-nexus-workflow-step', 'n/a')}")
    print(f"Answer     : {completion.choices[0].message.content}")


# ── 6. Multi-step workflow with streaming + header inspection ──────────

def workflow_example():
    """
    A multi-step workflow that chains requests, reads Nexus routing metadata
    at each step, and demonstrates how Nexus tracks cumulative cost.
    """
    workflow_id = "onboarding-flow-99"
    steps = [
        ("researcher", "List the top 3 Python web frameworks and their strengths."),
        ("architect", "Given those frameworks, which is best for a high-traffic API?"),
        ("tester", "Write a pytest test for a FastAPI health endpoint."),
    ]

    print("\n=== Multi-Step Workflow ===")

    for i, (role, prompt) in enumerate(steps, start=1):
        raw = client.chat.completions.with_raw_response.create(
            model="auto",
            messages=[{"role": "user", "content": prompt}],
            max_tokens=512,
            extra_headers={
                "X-Workflow-ID": workflow_id,
                "X-Agent-Role": role,
                "X-Step-Number": str(i),
                "X-Budget": "5.00",
            },
        )

        completion = raw.parse()
        tier = raw.headers.get("x-nexus-tier", "?")
        cost = raw.headers.get("x-nexus-cost", "?")
        model = raw.headers.get("x-nexus-model", "?")

        preview = completion.choices[0].message.content[:80].replace("\n", " ")
        print(f"  Step {i} [{role:>12}] → tier={tier}, model={model}, cost=${cost}")
        print(f"    {preview}...")


# ── Run all examples ────────────────────────────────────────────────────

if __name__ == "__main__":
    basic_completion()
    streaming_completion()
    nexus_routed_completion()
    read_nexus_headers()
    workflow_example()
