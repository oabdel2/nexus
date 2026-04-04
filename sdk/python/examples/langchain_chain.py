"""Using Nexus with LangChain chains."""
from nexus_gateway.langchain import ChatNexus
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.output_parsers import StrOutputParser

# Create Nexus-routed LLM with budget
llm = ChatNexus(
    nexus_url="http://localhost:8080",
    workflow_id="langchain-demo",
    budget=2.0,
)

# Build a chain
prompt = ChatPromptTemplate.from_messages([
    ("system", "You are a helpful coding assistant."),
    ("user", "{question}"),
])

chain = prompt | llm | StrOutputParser()

# Run — Nexus auto-routes based on complexity
simple = chain.invoke({"question": "What does `len()` do in Python?"})
print(f"Simple question: {simple[:100]}...")

complex_q = chain.invoke({
    "question": "Design a distributed task queue with priority scheduling, "
    "dead letter handling, and exactly-once delivery guarantees. "
    "Include the full architecture and implementation plan."
})
print(f"Complex question: {complex_q[:100]}...")

# Check what tiers were used
print(f"\nLLM type: {llm._llm_type}")
print(f"Steps executed: {llm._step}")
