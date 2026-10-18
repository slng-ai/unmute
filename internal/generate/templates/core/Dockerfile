# Written by `unmute compile`. The image this agent deploys, and `unmute dev` runs.
FROM python:3.12-slim

WORKDIR /app
{{.BuildTools}}RUN pip install --no-cache-dir uv
COPY pyproject.toml ./
RUN uv pip install --system --no-cache -r pyproject.toml

RUN useradd --home-dir /app --shell /usr/sbin/nologin appuser \
 && chown appuser:appuser /app
COPY --chown=appuser:appuser . .
USER appuser
CMD {{.Command}}
