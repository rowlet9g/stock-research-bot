from __future__ import annotations

from dataclasses import dataclass
from datetime import date, timedelta
from typing import Any

import requests


OPENDART_BASE_URL = "https://opendart.fss.or.kr/api"


@dataclass(frozen=True)
class Disclosure:
    corp_code: str
    corp_name: str
    report_name: str
    receipt_no: str
    receipt_date: str
    submitter: str | None


class OpenDartClient:
    def __init__(self, api_key: str, timeout_seconds: int = 10) -> None:
        self.api_key = api_key
        self.timeout_seconds = timeout_seconds

    def recent_disclosures(
        self,
        corp_code: str | None = None,
        days: int = 30,
        page_count: int = 20,
    ) -> list[Disclosure]:
        end_date = date.today()
        begin_date = end_date - timedelta(days=days)
        params: dict[str, Any] = {
            "crtfc_key": self.api_key,
            "bgn_de": begin_date.strftime("%Y%m%d"),
            "end_de": end_date.strftime("%Y%m%d"),
            "page_count": page_count,
        }
        if corp_code:
            params["corp_code"] = corp_code

        response = requests.get(
            f"{OPENDART_BASE_URL}/list.json",
            params=params,
            timeout=self.timeout_seconds,
        )
        response.raise_for_status()
        payload = response.json()

        status = payload.get("status")
        if status not in {"000", "013"}:
            message = payload.get("message", "OpenDART request failed.")
            raise RuntimeError(f"OpenDART error {status}: {message}")

        return [
            Disclosure(
                corp_code=item.get("corp_code", ""),
                corp_name=item.get("corp_name", ""),
                report_name=item.get("report_nm", ""),
                receipt_no=item.get("rcept_no", ""),
                receipt_date=item.get("rcept_dt", ""),
                submitter=item.get("flr_nm"),
            )
            for item in payload.get("list", [])
        ]
