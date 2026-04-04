"""Using Nexus with CrewAI agents — each agent gets budget-aware routing."""
from nexus_gateway.crewai import nexus_llm
# from crewai import Agent, Task, Crew  # Uncomment when crewai is installed

# Each agent gets its own budget-aware LLM
researcher_llm = nexus_llm(
    workflow_id="research-crew",
    budget=3.0,  # $3 budget for research
)

writer_llm = nexus_llm(
    workflow_id="writing-crew", 
    budget=2.0,  # $2 budget for writing
)

reviewer_llm = nexus_llm(
    workflow_id="review-crew",
    budget=1.0,  # $1 budget for review
)

print("CrewAI LLMs configured with Nexus routing:")
print(f"  Researcher: budget=$3.00, workflow=research-crew")
print(f"  Writer:     budget=$2.00, workflow=writing-crew")
print(f"  Reviewer:   budget=$1.00, workflow=review-crew")
print()
print("Each agent's requests are independently routed through Nexus.")
print("Complex research queries → premium models")
print("Simple formatting tasks → economy models")
print("Budget tracked per-agent, downgrades automatically when low.")

# Example CrewAI setup (uncomment with crewai installed):
#
# researcher = Agent(
#     role="Senior Researcher",
#     goal="Find cutting-edge information",
#     llm=researcher_llm,
# )
#
# writer = Agent(
#     role="Technical Writer",
#     goal="Write clear documentation",
#     llm=writer_llm,
# )
#
# crew = Crew(agents=[researcher, writer], tasks=[...])
# result = crew.kickoff()
