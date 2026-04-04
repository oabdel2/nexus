"""Basic usage of the Nexus Python SDK."""
from nexus_gateway import NexusClient

client = NexusClient("http://localhost:8080")

# One-shot request (auto-routed)
response = client.chat([
    {"role": "user", "content": "What is 2+2?"}
])
print(f"Answer: {response.content}")
print(f"Routed to: {response.model} (tier: {response.tier})")
print(f"Latency: {response.latency_ms:.0f}ms, Cache hit: {response.cache_hit}")

# Multi-step workflow with budget
with client.workflow(budget=1.0) as wf:
    # Step 1: Simple question → economy tier
    step1 = wf.chat([
        {"role": "user", "content": "List 3 Python web frameworks"}
    ])
    print(f"\nStep 1 → {step1.tier}: {step1.content[:80]}...")

    # Step 2: Complex analysis → premium tier
    step2 = wf.chat([
        {"role": "system", "content": "You are a senior software architect."},
        {"role": "user", "content": f"Compare these frameworks for a microservices architecture: {step1.content}. Consider scalability, async support, and ecosystem maturity."}
    ])
    print(f"Step 2 → {step2.tier}: {step2.content[:80]}...")

    # Step 3: Implementation → mid tier
    step3 = wf.chat([
        {"role": "user", "content": f"Write a basic REST API with the best framework from: {step2.content[:200]}"}
    ])
    print(f"Step 3 → {step3.tier}: {step3.content[:80]}...")

    # Submit feedback
    wf.feedback(rating=0.9, comment="Good routing decisions")
    
    # Print workflow summary
    print(f"\nWorkflow summary: {wf.summary}")

client.close()
