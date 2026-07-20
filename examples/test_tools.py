from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
import sys

sys.dont_write_bytecode = True

ROOT = Path(__file__).parent
PACKAGES = ("simple-prompt", "single-task", "task-groups", "subagents")
TOOLS = (
    "lookup_customer",
    "create_customer",
    "check_availability",
    "book_appointment",
    "cancel_appointment",
)


def load(package, tool):
    path = ROOT / package / "tools" / f"{tool}.py"
    spec = spec_from_file_location(f"{package}_{tool}", path)
    module = module_from_spec(spec)
    spec.loader.exec_module(module)
    return getattr(module, tool)


def raises_value_error(call):
    try:
        call()
    except ValueError:
        return
    raise AssertionError("expected ValueError")


def check_drift():
    canonical = ROOT / PACKAGES[0] / "tools"
    for package in PACKAGES[1:]:
        for tool in TOOLS:
            for suffix in (".py", ".yaml"):
                expected = (canonical / f"{tool}{suffix}").read_bytes()
                actual = (ROOT / package / "tools" / f"{tool}{suffix}").read_bytes()
                assert actual == expected, f"{package}/{tool}{suffix} drifted"


def check_package(package):
    lookup = load(package, "lookup_customer")
    create = load(package, "create_customer")
    availability = load(package, "check_availability")
    book = load(package, "book_appointment")
    cancel = load(package, "cancel_appointment")

    assert lookup("+1 (555) 010-101")["customer_id"] == "cus_1001"
    assert lookup("+1 555 999 0000") == {
        "found": False,
        "customer_id": "",
        "name": "",
    }
    raises_value_error(lambda: lookup("123"))

    customer = create("Taylor Reed", "+1 555 999 0000")
    assert customer["created"] and customer["customer_id"].startswith("cus_")
    assert create("Taylor Reed", "+1 555 999 0000") == customer
    raises_value_error(lambda: create("", "+1 555 999 0000"))

    slots = availability("haircut", "2026-08-03")["slots"]
    assert len(slots) == 3
    assert availability("haircut", "2026-08-02") == {"slots": []}
    raises_value_error(lambda: availability("massage", "2026-08-03"))
    raises_value_error(lambda: availability("haircut", "August 3"))

    appointment = book(customer["customer_id"], "haircut", slots[0]["slot_id"])
    assert appointment["status"] == "booked"
    raises_value_error(lambda: book("unknown", "haircut", slots[0]["slot_id"]))
    raises_value_error(lambda: book(customer["customer_id"], "haircut", "unknown"))

    assert cancel(appointment["appointment_id"])["status"] == "cancelled"
    assert cancel("unknown") == {"appointment_id": "unknown", "status": "not_found"}


def main():
    check_drift()
    for package in PACKAGES:
        check_package(package)
    print(f"ok: {len(TOOLS)} tools in {len(PACKAGES)} packages")


if __name__ == "__main__":
    main()
