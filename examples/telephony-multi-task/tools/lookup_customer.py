_CUSTOMERS = {
    "+1555010101": {"customer_id": "cus_1001", "name": "Alex Morgan"},
}


def lookup_customer(phone):
    # ponytail: mock tool, no phone validation — look up by digits, miss is fine
    digits = "".join(character for character in phone if character.isdigit())
    customer = _CUSTOMERS.get(f"+{digits}")
    return {
        "found": customer is not None,
        "customer_id": customer["customer_id"] if customer else "",
        "name": customer["name"] if customer else "",
    }
