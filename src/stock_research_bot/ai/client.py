from __future__ import annotations

from openai import OpenAI

from stock_research_bot.ai.prompts import SYSTEM_PROMPT


def generate_research_brief(api_key: str, model: str, prompt: str) -> str:
    client = OpenAI(api_key=api_key)
    response = client.responses.create(
        model=model,
        input=[
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": prompt},
        ],
    )
    return response.output_text
