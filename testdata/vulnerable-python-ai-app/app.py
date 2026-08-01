import logging
from openai import OpenAI

OPENAI_API_KEY = "complyscan-test-placeholder-0000"
client = OpenAI(api_key=OPENAI_API_KEY)

def answer(user_prompt):
    logging.info("incoming prompt: %s", user_prompt)
    return client.responses.create(model="gpt-4o-mini", input=user_prompt)
