import uuid
import time
from contextlib import contextmanager
from typing import Optional, Generator
import httpx
from nexus_gateway.types import ChatMessage, ChatResponse, FeedbackPayload


class NexusWorkflow:
    """Context manager for multi-step workflows."""
    
    def __init__(self, client: "NexusClient", workflow_id: Optional[str] = None, budget: Optional[float] = None):
        self.client = client
        self.workflow_id = workflow_id or f"wf-{uuid.uuid4().hex[:12]}"
        self.budget = budget
        self.step = 0
        self.responses: list[ChatResponse] = []
        self.total_cost = 0.0
    
    def chat(self, messages: list[ChatMessage] | list[dict], **kwargs) -> ChatResponse:
        """Send a chat request as part of this workflow."""
        self.step += 1
        # Convert dicts to ChatMessage if needed
        if messages and isinstance(messages[0], dict):
            messages = [ChatMessage(**m) for m in messages]
        
        response = self.client._chat(
            messages=messages,
            workflow_id=self.workflow_id,
            step=self.step,
            budget=self.budget,
            **kwargs
        )
        self.responses.append(response)
        return response
    
    def feedback(self, rating: float, comment: Optional[str] = None, step: Optional[int] = None):
        """Submit feedback for a step in this workflow."""
        self.client.feedback(FeedbackPayload(
            workflow_id=self.workflow_id,
            step=step or self.step,
            rating=rating,
            comment=comment,
        ))
    
    @property
    def summary(self) -> dict:
        """Get a summary of this workflow's routing decisions."""
        tiers = {}
        for r in self.responses:
            tiers[r.tier] = tiers.get(r.tier, 0) + 1
        return {
            "workflow_id": self.workflow_id,
            "steps": self.step,
            "total_cost": self.total_cost,
            "tier_distribution": tiers,
            "cache_hits": sum(1 for r in self.responses if r.cache_hit),
        }


class NexusClient:
    """Client for the Nexus Inference Optimization Gateway.
    
    Usage:
        client = NexusClient("http://localhost:8080")
        
        # Simple one-shot
        response = client.chat([{"role": "user", "content": "Hello"}])
        
        # Multi-step workflow
        with client.workflow(budget=1.0) as wf:
            plan = wf.chat([{"role": "user", "content": "Plan a project"}])
            code = wf.chat([{"role": "user", "content": f"Implement: {plan.content}"}])
            review = wf.chat([{"role": "user", "content": f"Review: {code.content}"}])
            wf.feedback(rating=0.9, comment="Great routing!")
    """
    
    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        timeout: float = 120.0,
        default_model: str = "auto",
    ):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.default_model = default_model
        self._http = httpx.Client(timeout=timeout)
    
    def chat(self, messages: list[ChatMessage] | list[dict], **kwargs) -> ChatResponse:
        """Send a one-shot chat request."""
        if messages and isinstance(messages[0], dict):
            messages = [ChatMessage(**m) for m in messages]
        return self._chat(messages=messages, **kwargs)
    
    @contextmanager
    def workflow(
        self,
        workflow_id: Optional[str] = None,
        budget: Optional[float] = None,
    ) -> Generator[NexusWorkflow, None, None]:
        """Create a multi-step workflow context."""
        wf = NexusWorkflow(self, workflow_id=workflow_id, budget=budget)
        yield wf
    
    def feedback(self, payload: FeedbackPayload):
        """Submit feedback for a workflow step."""
        self._http.post(
            f"{self.base_url}/v1/feedback",
            json={
                "workflow_id": payload.workflow_id,
                "step": payload.step,
                "rating": payload.rating,
                "comment": payload.comment,
            },
        )
    
    def health(self) -> dict:
        """Check gateway health."""
        r = self._http.get(f"{self.base_url}/health")
        return r.json()
    
    def metrics(self) -> str:
        """Get Prometheus metrics."""
        r = self._http.get(f"{self.base_url}/metrics")
        return r.text
    
    def _chat(
        self,
        messages: list[ChatMessage],
        workflow_id: Optional[str] = None,
        step: Optional[int] = None,
        budget: Optional[float] = None,
        model: Optional[str] = None,
        temperature: Optional[float] = None,
        max_tokens: Optional[int] = None,
    ) -> ChatResponse:
        """Internal chat method with full header control."""
        headers = {}
        if workflow_id:
            headers["X-Workflow-ID"] = workflow_id
        if step is not None:
            headers["X-Step-Number"] = str(step)
        if budget is not None:
            headers["X-Budget"] = str(budget)
        
        body = {
            "model": model or self.default_model,
            "messages": [{"role": m.role, "content": m.content} for m in messages],
        }
        if temperature is not None:
            body["temperature"] = temperature
        if max_tokens is not None:
            body["max_tokens"] = max_tokens
        
        start = time.monotonic()
        r = self._http.post(
            f"{self.base_url}/v1/chat/completions",
            json=body,
            headers=headers,
        )
        latency = (time.monotonic() - start) * 1000
        
        data = r.json()
        r.raise_for_status()
        
        content = ""
        if data.get("choices"):
            content = data["choices"][0].get("message", {}).get("content", "")
        
        return ChatResponse(
            content=content,
            model=data.get("model", ""),
            tier=data.get("tier", "unknown"),
            usage=data.get("usage", {}),
            latency_ms=latency,
            cache_hit=data.get("cache_hit", False),
            raw=data,
        )
    
    def close(self):
        """Close the HTTP client."""
        self._http.close()
    
    def __enter__(self):
        return self
    
    def __exit__(self, *args):
        self.close()
