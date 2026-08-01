import logging
from openai import OpenAI

client = OpenAI(api_key="sk-proj-1234567890abcdefghijklmnop")

def answer(user_prompt):
    logging.info("incoming prompt: %s", user_prompt)
    return client.responses.create(model="gpt-4o-mini", input=user_prompt)
