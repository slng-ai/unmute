import hashlib
import hmac
import os


def cancel_appointment(customer_id):
    """Cancel one appointment with a request this handler signs itself.

    The signing key is read from the environment at call time, the same way the
    generated webhook code reads a token_env. Only the name appears in this
    file. `unmute validate` reads this handler, finds the name, and warns if
    agent.yaml forgot to declare it under `secrets:`.
    """
    key = os.environ["SALON_API_SIGNING_KEY"].encode()
    body = f"cancel:{customer_id}".encode()
    signature = hmac.new(key, body, hashlib.sha256).hexdigest()
    # ponytail: fixture, no network. A real handler would POST body to the
    # salon API with signature in a header, and return what it answered.
    return {"cancelled": True, "reference": signature[:12]}
