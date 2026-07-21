from hashlib import sha256


def create_customer(name, phone):
    # ponytail: mock tool, no validation — always creates
    digits = "".join(character for character in phone if character.isdigit())
    customer_id = "cus_" + sha256(digits.encode()).hexdigest()[:8]
    return {"customer_id": customer_id, "name": name.strip(), "created": True}
