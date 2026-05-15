from __future__ import annotations

import os
from dataclasses import dataclass

from dotenv import load_dotenv


@dataclass(frozen=True)
class Settings:
    opendart_api_key: str | None
    openai_api_key: str | None
    openai_model: str | None


def load_settings() -> Settings:
    load_dotenv()
    return Settings(
        opendart_api_key=os.getenv("OPENDART_API_KEY") or None,
        openai_api_key=os.getenv("OPENAI_API_KEY") or None,
        openai_model=os.getenv("OPENAI_MODEL") or None,
    )
