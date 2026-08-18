# Kalita Builder worker image (product mission): the Business Expert /
# Conversational Builder — Ask Kalita. Stdlib-only Python; the LLM boundary is
# env (KALITA_LLM_URL) — unset = deterministic mode.
FROM python:3.12-slim
WORKDIR /opt
COPY workers/builder /opt/workers/builder
COPY workers/agent_runner /opt/workers/agent_runner   # promptbox (prompt isolation)
COPY frameworks /opt/frameworks                        # curated business knowledge (sha256-verified at boot)
WORKDIR /opt/workers/builder
ENV KALITA_FRAMEWORKS_DIR=/opt/frameworks
EXPOSE 8400
CMD ["python", "builder.py"]
