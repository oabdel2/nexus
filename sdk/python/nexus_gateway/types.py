from dataclasses import dataclass, field
from typing import Optional

@dataclass
class ChatMessage:
    role: str  # "system", "user", "assistant"
    content: str

@dataclass
class ChatResponse:
    content: str
    model: str
    tier: str
    usage: dict = field(default_factory=dict)
    latency_ms: float = 0.0
    cache_hit: bool = False
    raw: dict = field(default_factory=dict)

@dataclass
class FeedbackPayload:
    workflow_id: str
    step: int
    rating: float  # 0.0 to 1.0
    comment: Optional[str] = None
