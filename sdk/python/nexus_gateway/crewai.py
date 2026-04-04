"""CrewAI integration for Nexus Gateway.

Usage:
    from nexus_gateway.crewai import nexus_llm
    from crewai import Agent
    
    llm = nexus_llm(budget=5.0)
    agent = Agent(role="Researcher", llm=llm, ...)
"""

from typing import Optional

try:
    from nexus_gateway.langchain import ChatNexus
except ImportError:
    raise ImportError(
        "CrewAI integration requires langchain-core. "
        "Install with: pip install nexus-gateway[crewai]"
    )


def nexus_llm(
    nexus_url: str = "http://localhost:8080",
    workflow_id: Optional[str] = None,
    budget: Optional[float] = None,
    model: str = "auto",
) -> ChatNexus:
    """Create a Nexus-routed LLM for use with CrewAI agents.
    
    CrewAI uses LangChain under the hood, so this returns
    a ChatNexus instance configured for the workflow.
    
    Args:
        nexus_url: Nexus gateway URL
        workflow_id: Optional workflow ID (auto-generated if not set)
        budget: Optional budget cap in dollars
        model: Model name or "auto" for adaptive routing
    
    Returns:
        ChatNexus instance ready for CrewAI Agent
    """
    return ChatNexus(
        nexus_url=nexus_url,
        workflow_id=workflow_id,
        budget=budget,
        model_name=model,
    )
