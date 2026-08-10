from fastapi import FastAPI
from openai import OpenAI

app = FastAPI()
client = OpenAI()

TOOLS = {
    "lookup_order": lambda order_id: {"id": order_id, "status": "shipped"},
}


def draft_reply(ticket: str) -> str:
    response = client.responses.create(
        model="support-model",
        input=ticket,
        tools=[{"type": "function", "name": "lookup_order"}],
    )
    return response.output_text


def require_agent_approval(draft: str, approved: bool) -> str:
    if not approved:
        raise PermissionError("A support agent must approve the draft")
    return draft


@app.post("/draft-reply")
def create_reply(ticket: str, approved: bool) -> dict[str, str]:
    draft = draft_reply(ticket)
    return {"reply": require_agent_approval(draft, approved)}
