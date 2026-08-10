FROM python:3.12-slim

WORKDIR /app
RUN pip install --no-cache-dir uv

COPY pyproject.toml ./
RUN uv pip install --system .

COPY . .

# LiveKit Cloud / the worker runner start agent.py; `python agent.py dev` for local runs.
CMD ["python", "agent.py", "start"]
