"""Trading agent role presets for multi-agent investment analysis.

This module provides specialized roles for trading template workflows:

trading_analysis workflow:
- fundamental_analyst: 10-K/10-Q analysis, valuation metrics
- technical_analyst: Price patterns, indicators, support/resistance
- sentiment_analyst: News, social media, options flow sentiment
- bull_researcher: Builds bullish investment thesis
- bear_researcher: Builds bearish counter-thesis
- risk_analyst: Position sizing, risk factors, hedging
- portfolio_manager: Final TradeIntent synthesis

event_catalyst workflow:
- earnings_analyst: Whisper numbers, estimate revisions, guidance patterns
- options_analyst: IV rank, skew, unusual activity, implied move
- event_historian: Historical event reactions, sector correlations
- catalyst_synthesizer: Final event positioning recommendation

regime_detection workflow:
- macro_analyst: Fed policy, yields, dollar, economic indicators
- sector_analyst: Sector rotation, relative strength, factor trends
- volatility_analyst: VIX regime, term structure, realized vol
- regime_synthesizer: Combined regime classification

famous_investors workflow:
- warren_buffett_investor: Owner earnings, moat, management quality, circle of competence
- ben_graham_investor: Net-net, margin of safety, balance sheet fortress
- charlie_munger_investor: Mental models, inversion, quality over price
- peter_lynch_investor: GARP, PEG ratio, stock categorization
- phil_fisher_investor: Scuttlebutt method, management quality, long-term growth
- michael_burry_investor: Deep value, short thesis, hidden risks, contrarian
- bill_ackman_investor: Activist lens, catalysts, downside framing
- investor_panel_synthesizer: Consensus and debate summary

news_monitor workflow:
- news_searcher: General financial news search
- market_news_searcher: Analyst ratings and price targets
- social_searcher: Social media sentiment
- news_sentiment_synthesizer: Aggregated sentiment report
"""

from .fundamental_analyst import FUNDAMENTAL_ANALYST_PRESET
from .technical_analyst import TECHNICAL_ANALYST_PRESET
from .sentiment_analyst import SENTIMENT_ANALYST_PRESET
from .bull_researcher import BULL_RESEARCHER_PRESET
from .bear_researcher import BEAR_RESEARCHER_PRESET
from .risk_analyst import RISK_ANALYST_PRESET
from .portfolio_manager import PORTFOLIO_MANAGER_PRESET

# Event catalyst roles
from .earnings_analyst import EARNINGS_ANALYST_PRESET
from .options_analyst import OPTIONS_ANALYST_PRESET
from .event_historian import EVENT_HISTORIAN_PRESET
from .catalyst_synthesizer import CATALYST_SYNTHESIZER_PRESET

# Regime detection roles
from .macro_analyst import MACRO_ANALYST_PRESET
from .sector_analyst import SECTOR_ANALYST_PRESET
from .volatility_analyst import VOLATILITY_ANALYST_PRESET
from .regime_synthesizer import REGIME_SYNTHESIZER_PRESET

# Famous investors roles
from .warren_buffett_investor import WARREN_BUFFETT_INVESTOR_PRESET
from .ben_graham_investor import BEN_GRAHAM_INVESTOR_PRESET
from .charlie_munger_investor import CHARLIE_MUNGER_INVESTOR_PRESET
from .peter_lynch_investor import PETER_LYNCH_INVESTOR_PRESET
from .phil_fisher_investor import PHIL_FISHER_INVESTOR_PRESET
from .michael_burry_investor import MICHAEL_BURRY_INVESTOR_PRESET
from .bill_ackman_investor import BILL_ACKMAN_INVESTOR_PRESET
from .investor_panel_synthesizer import INVESTOR_PANEL_SYNTHESIZER_PRESET

# News monitor roles
from .news_searcher import NEWS_SEARCHER_PRESET
from .market_news_searcher import MARKET_NEWS_SEARCHER_PRESET
from .social_searcher import SOCIAL_SEARCHER_PRESET
from .news_sentiment_synthesizer import NEWS_SENTIMENT_SYNTHESIZER_PRESET

__all__ = [
    # trading_analysis roles
    "FUNDAMENTAL_ANALYST_PRESET",
    "TECHNICAL_ANALYST_PRESET",
    "SENTIMENT_ANALYST_PRESET",
    "BULL_RESEARCHER_PRESET",
    "BEAR_RESEARCHER_PRESET",
    "RISK_ANALYST_PRESET",
    "PORTFOLIO_MANAGER_PRESET",
    # event_catalyst roles
    "EARNINGS_ANALYST_PRESET",
    "OPTIONS_ANALYST_PRESET",
    "EVENT_HISTORIAN_PRESET",
    "CATALYST_SYNTHESIZER_PRESET",
    # regime_detection roles
    "MACRO_ANALYST_PRESET",
    "SECTOR_ANALYST_PRESET",
    "VOLATILITY_ANALYST_PRESET",
    "REGIME_SYNTHESIZER_PRESET",
    # famous_investors roles
    "WARREN_BUFFETT_INVESTOR_PRESET",
    "BEN_GRAHAM_INVESTOR_PRESET",
    "CHARLIE_MUNGER_INVESTOR_PRESET",
    "PETER_LYNCH_INVESTOR_PRESET",
    "PHIL_FISHER_INVESTOR_PRESET",
    "MICHAEL_BURRY_INVESTOR_PRESET",
    "BILL_ACKMAN_INVESTOR_PRESET",
    "INVESTOR_PANEL_SYNTHESIZER_PRESET",
    # news_monitor roles
    "NEWS_SEARCHER_PRESET",
    "MARKET_NEWS_SEARCHER_PRESET",
    "SOCIAL_SEARCHER_PRESET",
    "NEWS_SENTIMENT_SYNTHESIZER_PRESET",
]
