from __future__ import annotations

from pathlib import Path

import pandas as pd
import streamlit as st

from stock_research_bot.ai.client import generate_research_brief
from stock_research_bot.ai.prompts import build_stock_brief_prompt
from stock_research_bot.analysis.rules import evaluate_price_snapshot
from stock_research_bot.config import load_settings
from stock_research_bot.data.dart import OpenDartClient
from stock_research_bot.data.yahoo import build_price_snapshot


WATCHLIST_EXAMPLE = Path("data/watchlist.example.csv")


def load_watchlist(uploaded_file) -> pd.DataFrame:
    if uploaded_file is not None:
        return pd.read_csv(uploaded_file)
    return pd.read_csv(WATCHLIST_EXAMPLE)


st.set_page_config(page_title="AI Stock Research Bot", layout="wide")
st.title("AI Stock Research Bot")

settings = load_settings()

with st.sidebar:
    st.subheader("데이터")
    uploaded_file = st.file_uploader("관심종목 CSV", type=["csv"])
    run_yahoo = st.button("Yahoo 가격 갱신")
    run_ai = st.button("AI 브리핑 생성")

watchlist = load_watchlist(uploaded_file)
st.dataframe(watchlist, use_container_width=True)

selected_name = st.selectbox("분석할 종목", watchlist["name"].tolist())
selected = watchlist.loc[watchlist["name"] == selected_name].iloc[0]

user_thesis = st.text_area(
    "투자 가설",
    placeholder="예: 실적 턴어라운드가 시작됐다고 보고 관심종목에 편입했다.",
)

snapshot = None
signals = []
disclosures = []

if run_yahoo:
    snapshot = build_price_snapshot(str(selected["yahoo_ticker"]))
    signals = evaluate_price_snapshot(snapshot)

    col1, col2, col3, col4 = st.columns(4)
    col1.metric("현재가", snapshot.last_price)
    col2.metric("1일 변동률", None if snapshot.change_pct_1d is None else f"{snapshot.change_pct_1d:.2f}%")
    col3.metric("20일선", snapshot.ma20)
    col4.metric("60일선", snapshot.ma60)

    for signal in signals:
        if signal.level == "warning":
            st.warning(f"{signal.title}: {signal.detail}")
        elif signal.level == "error":
            st.error(f"{signal.title}: {signal.detail}")
        else:
            st.info(f"{signal.title}: {signal.detail}")

if settings.opendart_api_key and str(selected.get("dart_corp_code", "")).strip():
    dart = OpenDartClient(settings.opendart_api_key)
    try:
        disclosures = dart.recent_disclosures(str(selected["dart_corp_code"]), days=30)
    except Exception as exc:
        st.error(f"DART 공시 수집 실패: {exc}")
else:
    st.caption("DART API 키와 dart_corp_code가 있으면 최근 공시를 함께 분석합니다.")

if disclosures:
    st.subheader("최근 DART 공시")
    st.dataframe(pd.DataFrame([item.__dict__ for item in disclosures]), use_container_width=True)

if run_ai:
    if snapshot is None:
        snapshot = build_price_snapshot(str(selected["yahoo_ticker"]))
        signals = evaluate_price_snapshot(snapshot)

    prompt = build_stock_brief_prompt(
        name=str(selected["name"]),
        snapshot=snapshot,
        signals=signals,
        disclosures=disclosures,
        user_thesis=user_thesis or None,
    )

    st.subheader("AI 입력 프롬프트")
    st.code(prompt, language="text")

    if settings.openai_api_key and settings.openai_model:
        with st.spinner("AI 브리핑 생성 중"):
            brief = generate_research_brief(settings.openai_api_key, settings.openai_model, prompt)
        st.subheader("AI 브리핑")
        st.markdown(brief)
    else:
        st.info("OPENAI_API_KEY와 OPENAI_MODEL을 .env에 설정하면 AI 브리핑을 생성합니다.")
