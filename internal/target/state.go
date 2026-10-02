package target

// StatePython is the interpreter line `unmute` reads a package's state.py
// with. It is the same line the Twilio project runs on.
const StatePython = TwilioPython

// StatePins are the exact versions `unmute validate` and `unmute compile`
// install to read a package's state.py. The author may import Pydantic, and
// three pydantic-extra-types modules (phone_numbers, currency_code,
// language_code), which need phonenumbers and pycountry. email-validator backs
// EmailStr and NameEmail. Moving a pin here changes every recorded schema
// digest, so the fixtures are re-recorded in the same commit.
var StatePins = map[string]string{
	"email-validator":      "2.3.0",
	"phonenumbers":         "9.0.40",
	"pycountry":            "26.2.16",
	"pydantic":             "2.13.5",
	"pydantic-extra-types": "2.11.1",
}
