# Nexus Gateway Python SDK

Python client for the [Nexus Inference Optimization Gateway](https://github.com/nexus-gateway/nexus).

## Install

```bash
pip install nexus-gateway                    # Core SDK
pip install nexus-gateway[langchain]         # + LangChain integration
pip install nexus-gateway[crewai]            # + CrewAI integration
pip install nexus-gateway[all]               # Everything
```

## Quick Start

```python
from nexus_gateway import NexusClient

client = NexusClient("http://localhost:8080")

# One-shot (auto-routed by complexity)
response = client.chat([{"role": "user", "content": "Hello!"}])

# Multi-step workflow with budget
with client.workflow(budget=1.0) as wf:
    plan = wf.chat([{"role": "user", "content": "Plan a project"}])
    code = wf.chat([{"role": "user", "content": f"Implement: {plan.content}"}])
    wf.feedback(rating=0.9)
    print(wf.summary)
```

## LangChain

```python
from nexus_gateway.langchain import ChatNexus

llm = ChatNexus(nexus_url="http://localhost:8080", budget=2.0)
chain = prompt | llm | parser
result = chain.invoke({"question": "..."})
```

## CrewAI

```python
from nexus_gateway.crewai import nexus_llm
from crewai import Agent

agent = Agent(role="Researcher", llm=nexus_llm(budget=3.0))
```

## Features

- 🔄 **Automatic workflow tracking** — step counting, workflow IDs
- 💰 **Budget management** — set caps, auto-downgrades when depleted
- 📊 **Routing transparency** — see which tier/model was selected
- 🔌 **Drop-in compatible** — works with LangChain, CrewAI, or standalone
- ⚡ **Feedback loop** — rate responses to improve future routing

## License

Apache-2.0
