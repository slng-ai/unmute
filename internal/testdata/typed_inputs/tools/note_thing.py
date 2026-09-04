"""Test handler: records nothing and says so."""


def note_thing(text: str, kind: str) -> dict:
    return {"saved": bool(text) and bool(kind)}
