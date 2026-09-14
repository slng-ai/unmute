"""The takeaway counter's two tools, backed by an in-process demo store.

The compiler copies this file once per tool (`tools/<tool_name>.py`), so a
module-level dict would give each copy its own private store and an order placed
through one would be invisible to the other. One module parked in `sys.modules`
under a name nothing else uses is the smallest thing every copy can reach.

The menu is a few lines of data rather than a database because the point of the
example is the live model and its backend, not the shop. Everything here runs in
process, so a run needs no service beyond the two model keys.
"""

import sys
import threading
import types

_fresh = types.ModuleType("unmute_takeaway_state")
vars(_fresh).update(orders=[], lock=threading.Lock())
_state = sys.modules.setdefault("unmute_takeaway_state", _fresh)

# name -> (price in pounds, on tonight, what to say about it).
# "crispy duck" is off, so a caller can hear the agent handle a dish it cannot
# sell without anybody editing this file.
_MENU = {
    "spring rolls": (4.50, True, "Two to a portion, vegetarian."),
    "prawn toast": (5.20, True, "Four pieces, contains prawn and egg."),
    "salt and pepper chicken": (8.90, True, "Can be coated in rice flour on request."),
    "sweet and sour chicken": (8.50, True, "Battered, served with the sauce on the side."),
    "beef in black bean sauce": (9.40, True, "Contains oyster sauce."),
    "chicken with cashew nuts": (9.10, True, "Contains cashews."),
    "kung pao chicken": (9.20, True, "Contains peanuts. Hot as standard."),
    "mixed vegetable chow mein": (7.80, True, "Contains egg noodles and oyster sauce."),
    "egg fried rice": (3.90, True, "Contains egg."),
    "steamed rice": (3.20, True, "No egg, no sauce."),
    "crispy aromatic duck": (16.50, False, "Off tonight, the kitchen ran out."),
}

# What the caller says is rarely what the menu calls it, and the model should
# not have to learn the menu to ask about it.
_ALIASES = {
    "duck": "crispy aromatic duck",
    "crispy duck": "crispy aromatic duck",
    "sweet and sour": "sweet and sour chicken",
    "cashew chicken": "chicken with cashew nuts",
    "chow mein": "mixed vegetable chow mein",
    "black bean beef": "beef in black bean sauce",
    "fried rice": "egg fried rice",
    "boiled rice": "steamed rice",
    "salt and pepper": "salt and pepper chicken",
}

_DELIVERY_CHARGE = 2.50
_DELIVERY_MINIMUM = 15.00


def _normalise(spoken):
    """Reduce what the caller said to the words the menu is keyed on."""
    return " ".join(str(spoken).lower().replace("-", " ").split())


def _match(spoken):
    """Find the menu name for a spoken one, or an empty string."""
    wanted = _normalise(spoken)
    if wanted in _MENU:
        return wanted
    if wanted in _ALIASES:
        return _ALIASES[wanted]
    # Last resort: one menu name that contains everything the caller said.
    hits = [name for name in _MENU if wanted and wanted in name]
    return hits[0] if len(hits) == 1 else ""


def look_up_dish(spoken_name):
    """Answer the one question the model must not answer from memory."""
    name = _match(spoken_name)
    if not name:
        return {
            "found": False,
            "name": "",
            "price": 0.0,
            "available": False,
            "note": "Not on the menu. Offer something close and check that instead.",
        }
    price, available, note = _MENU[name]
    return {"found": True, "name": name, "price": price, "available": available, "note": note}


def place_order(items, fulfilment, name):
    """Write the order and hand back what the model reads out.

    Every refusal here is one the prompt cannot prevent: a dish that went off
    between the lookup and the order, and a delivery under the minimum. The
    model gets the reason in `problem` so it can say it out loud.
    """
    lines = [_match(item) for item in items]
    if not lines or "" in lines:
        return _rejected("One of those dishes is not on the menu. Check each one first.")
    off = [line for line in lines if not _MENU[line][1]]
    if off:
        return _rejected(f"{off[0]} is off tonight. Offer something else.")

    total = round(sum(_MENU[line][0] for line in lines), 2)
    if fulfilment == "delivery":
        if total < _DELIVERY_MINIMUM:
            return _rejected(
                f"Delivery needs at least {_DELIVERY_MINIMUM:.2f} pounds of food. "
                f"This order is {total:.2f}. Offer collection or another dish."
            )
        total = round(total + _DELIVERY_CHARGE, 2)

    # A bigger order takes longer, and delivery adds the drive.
    wait = 15 + 5 * max(0, len(lines) - 2) + (30 if fulfilment == "delivery" else 0)
    with _state.lock:
        order_number = f"GW{len(_state.orders) + 101}"
        _state.orders.append(
            {"order_number": order_number, "name": name, "items": lines, "total": total}
        )
    return {
        "status": "placed",
        "order_number": order_number,
        "total": total,
        "wait_minutes": wait,
        "problem": "",
    }


def _rejected(problem):
    """One shape for every refusal, so the model reads the same fields back."""
    return {
        "status": "rejected",
        "order_number": "",
        "total": 0.0,
        "wait_minutes": 0,
        "problem": problem,
    }


def _demo():
    """Run the handlers on their own, with no compiler and no models."""
    assert look_up_dish("crispy duck")["available"] is False
    assert look_up_dish("sweet and sour")["name"] == "sweet and sour chicken"
    assert look_up_dish("lasagne")["found"] is False

    small = place_order(["spring rolls"], "delivery", "Sam")
    assert small["status"] == "rejected", small
    assert "Delivery needs" in small["problem"], small

    placed = place_order(
        ["salt and pepper chicken", "egg fried rice"], "collection", "Sam"
    )
    assert placed["status"] == "placed", placed
    assert placed["total"] == 12.80, placed
    assert placed["wait_minutes"] == 15, placed

    second = place_order(["crispy duck"], "collection", "Ada")
    assert second["status"] == "rejected", second

    print("takeaway demo store: every check passed")


if __name__ == "__main__":
    _demo()
