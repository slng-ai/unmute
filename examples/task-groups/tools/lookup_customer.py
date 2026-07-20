_CUSTOMERS = {
    "+1555010101": {"customer_id": "cus_1001", "name": "Alex Morgan"},
}


def lookup_customer(phone):
    digits = "".join(character for character in phone if character.isdigit())
    if not 10 <= len(digits) <= 15:
        raise ValueError("phone must contain 10 to 15 digits")
    customer = _CUSTOMERS.get(f"+{digits}")
    return {
        "found": customer is not None,
        "customer_id": customer["customer_id"] if customer else "",
        "name": customer["name"] if customer else "",
    }
