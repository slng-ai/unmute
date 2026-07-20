# Needs uv add langfuse opentelemetry-sdk

import os

from langfuse import Langfuse
from livekit.agents.telemetry import set_tracer_provider
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.util.types import AttributeValue


def setup_langfuse(
    metadata: dict[str, AttributeValue] | None = None,
    *,
    base_url: str | None = None,
    public_key: str | None = None,
    secret_key: str | None = None,
) -> TracerProvider:
    public_key = public_key or os.getenv("LANGFUSE_PUBLIC_KEY")
    secret_key = secret_key or os.getenv("LANGFUSE_SECRET_KEY")
    base_url = base_url or os.getenv("LANGFUSE_BASE_URL") or os.getenv("LANGFUSE_HOST")

    if not public_key or not secret_key or not base_url:
        raise ValueError(
            "LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY, and LANGFUSE_BASE_URL (or LANGFUSE_HOST) must be set"
        )

    trace_provider = TracerProvider()
    set_tracer_provider(trace_provider, metadata=metadata)

    Langfuse(
        public_key=public_key,
        secret_key=secret_key,
        base_url=base_url,
        tracer_provider=trace_provider,
        should_export_span=lambda span: True,
    )

    return trace_provider
