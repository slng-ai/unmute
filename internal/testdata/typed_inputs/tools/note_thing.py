"""Test handler: records nothing and says so."""


def note_thing(text: str) -> dict:
    return {"saved": bool(text)}
