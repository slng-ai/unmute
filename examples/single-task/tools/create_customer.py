from hashlib import sha256


def create_customer(name, phone):
    clean_name = name.strip()
    digits = "".join(character for character in phone if character.isdigit())
    if not clean_name:
        raise ValueError("name is required")
    if not 10 <= len(digits) <= 15:
        raise ValueError("phone must contain 10 to 15 digits")
    customer_id = "cus_" + sha256(digits.encode()).hexdigest()[:8]
    return {"customer_id": customer_id, "name": clean_name, "created": True}
