The number read back was wrong, or there was none. You need the caller's whole phone
number, with its country code: Faro only takes Spanish numbers, starting with +34.

- When they give a number, call check_number with it exactly as they said it. The
  application reads it back and waits.
- A clear yes to the number just read back: call check_number again with the same number.
  A different number: call check_number with the number they said.
