"""Demonstrates budget-aware routing with automatic tier downgrades."""
from nexus_gateway import NexusClient

client = NexusClient("http://localhost:8080")

print("=== Budget-Aware Workflow Demo ===\n")
print("Starting with $0.50 budget. Watch how Nexus adapts routing")
print("as the budget depletes.\n")

with client.workflow(budget=0.50) as wf:
    tasks = [
        ("Analyze the security implications of this authentication flow...", "Complex analysis"),
        ("Summarize the key points from the analysis above.", "Medium summary"),
        ("What's 2+2?", "Simple math"),
        ("Format this as a bullet list: apples, oranges, bananas", "Simple formatting"),
        ("Write a comprehensive threat model for a microservices architecture with OAuth2, mTLS, and service mesh...", "Complex but budget may be low"),
    ]
    
    for prompt, description in tasks:
        response = wf.chat([{"role": "user", "content": prompt}])
        print(f"  Step {wf.step}: {description}")
        print(f"    → Tier: {response.tier} | Model: {response.model}")
        print(f"    → Latency: {response.latency_ms:.0f}ms | Cache: {response.cache_hit}")
        print()
    
    print(f"Workflow Summary:")
    summary = wf.summary
    for k, v in summary.items():
        print(f"  {k}: {v}")

client.close()
