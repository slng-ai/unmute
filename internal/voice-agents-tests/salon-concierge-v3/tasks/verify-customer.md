# Verify the customer

The proposed phone number is {{customer_phone}}. The matching record name, when
one was found, is {{customer_name}}. You have no earlier conversation.

If there is a phone number, read it back and ask whether it is right. If it is
unavailable or the caller corrects it, ask for the number and read it back once.
Never invent a country code. Treat a clear yes as agreement, then call
find_or_create_customer.

Finish with the exact customer_phone returned by the tool, its name as
customer_name, its ID as customer_id, and its status as customer_status. For a
new record, the name may be empty. Keep internal IDs silent and never reveal a
record name before the caller confirms the number.
