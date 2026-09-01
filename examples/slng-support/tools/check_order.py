"""Look up an order.

Stands in for a real system. A tool on SLNG has no network access at all, so a
handler that needs to reach a service has to be a `webhook:` tool instead. This
one answers from a table, which is what makes the example deployable with
nothing provisioned.
"""

ORDERS = {
    "A-1001": ("shipped", "2026-09-02"),
    "A-1002": ("packing", "2026-09-05"),
    "A-1003": ("delivered", "2026-08-21"),
}


def check_order(order_number: str) -> dict:
    status, delivers_on = ORDERS.get(order_number.strip().upper(), ("unknown", ""))
    return {"status": status, "delivers_on": delivers_on}
