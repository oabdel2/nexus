"""LangChain integration for Nexus Gateway.

Usage:
    from nexus_gateway.langchain import ChatNexus
    
    llm = ChatNexus(nexus_url="http://localhost:8080")
    response = llm.invoke("What is 2+2?")
    
    # With workflow context
    llm_with_workflow = ChatNexus(
        nexus_url="http://localhost:8080",
        workflow_id="my-chain",
        budget=2.0,
    )
"""

from typing import Any, Optional, Iterator
try:
    from langchain_core.language_models.chat_models import BaseChatModel
    from langchain_core.messages import BaseMessage, AIMessage, HumanMessage, SystemMessage
    from langchain_core.outputs import ChatResult, ChatGeneration
    from langchain_core.callbacks import CallbackManagerForLLMRun
except ImportError:
    raise ImportError(
        "LangChain integration requires langchain-core. "
        "Install with: pip install nexus-gateway[langchain]"
    )

from nexus_gateway.client import NexusClient
from nexus_gateway.types import ChatMessage


def _convert_message(msg: BaseMessage) -> ChatMessage:
    """Convert LangChain message to Nexus ChatMessage."""
    if isinstance(msg, HumanMessage):
        role = "user"
    elif isinstance(msg, AIMessage):
        role = "assistant"
    elif isinstance(msg, SystemMessage):
        role = "system"
    else:
        role = "user"
    return ChatMessage(role=role, content=msg.content)


class ChatNexus(BaseChatModel):
    """LangChain ChatModel that routes through Nexus Gateway.
    
    Automatically handles workflow tracking, step counting,
    and budget management.
    """
    
    nexus_url: str = "http://localhost:8080"
    workflow_id: Optional[str] = None
    budget: Optional[float] = None
    model_name: str = "auto"
    _client: Optional[NexusClient] = None
    _step: int = 0
    
    class Config:
        arbitrary_types_allowed = True
    
    @property
    def _llm_type(self) -> str:
        return "nexus-gateway"
    
    @property
    def client(self) -> NexusClient:
        if self._client is None:
            self._client = NexusClient(
                base_url=self.nexus_url,
                default_model=self.model_name,
            )
        return self._client
    
    def _generate(
        self,
        messages: list[BaseMessage],
        stop: Optional[list[str]] = None,
        run_manager: Optional[CallbackManagerForLLMRun] = None,
        **kwargs: Any,
    ) -> ChatResult:
        self._step += 1
        nexus_messages = [_convert_message(m) for m in messages]
        
        response = self.client._chat(
            messages=nexus_messages,
            workflow_id=self.workflow_id,
            step=self._step,
            budget=self.budget,
            model=kwargs.get("model"),
        )
        
        message = AIMessage(content=response.content)
        generation = ChatGeneration(
            message=message,
            generation_info={
                "model": response.model,
                "tier": response.tier,
                "latency_ms": response.latency_ms,
                "cache_hit": response.cache_hit,
                "usage": response.usage,
            },
        )
        return ChatResult(generations=[generation])
    
    @property
    def _identifying_params(self) -> dict:
        return {
            "nexus_url": self.nexus_url,
            "model_name": self.model_name,
            "workflow_id": self.workflow_id,
        }
