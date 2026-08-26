"""The author's own handler, and the slng driver copies this text unchanged.

That is what keeps one file working on all three targets: LiveKit and Pipecat
import it and call the function, and SLNG introspects the Input and Output
classes the driver appends below it.
"""

ORDERS = {
    "A1": {"status": "shipped", "delivers_on": "2026-09-01"},
}


def check_order(order_id: str) -> dict:
    return ORDERS.get(order_id, {"status": "unknown", "delivers_on": ""})
