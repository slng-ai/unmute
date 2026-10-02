"""Read a package's state.py and print what unmute checks about it, as JSON.

unmute runs this through uv, with the pinned Pydantic, on every validate and
compile. It never runs during a call. Any warning while state.py loads is an
error, because the one Pydantic gives here is a field that shadows a BaseModel
attribute, and that field would break the generated state class.
"""

import importlib.util
import json
import sys
import warnings
from types import ModuleType
from typing import Annotated

from pydantic import BaseModel, TypeAdapter, ValidationError


def load(path: str) -> tuple[ModuleType, type[BaseModel]]:
    """Import state.py as the module `state` and return it with its State class.

    Args:
        path: The state.py file.

    Returns:
        The module and its State class.
    """
    spec = importlib.util.spec_from_file_location("state", path)
    if spec is None or spec.loader is None:
        raise SystemExit(f"{path} cannot be loaded as a Python module")
    module = importlib.util.module_from_spec(spec)
    sys.modules["state"] = module
    spec.loader.exec_module(module)
    state = getattr(module, "State", None)
    if not (isinstance(state, type) and issubclass(state, BaseModel)):
        raise SystemExit(
            "state.py defines no class State that subclasses pydantic.BaseModel"
        )
    return module, state


def bad_defaults(state: type[BaseModel]) -> list[dict[str, str]]:
    """Check each default against its own field type, which Pydantic never does.

    Args:
        state: The State class.

    Returns:
        One entry per default its own type refuses.
    """
    out = []
    for name, info in state.model_fields.items():
        if info.is_required():
            continue
        annotation = (
            Annotated[(info.annotation, *info.metadata)]
            if info.metadata
            else info.annotation
        )
        try:
            TypeAdapter(annotation).validate_python(
                info.get_default(call_default_factory=True)
            )
        except ValidationError as error:
            out.append({"name": name, "error": error.errors()[0]["msg"]})
    return out


def main(path: str) -> None:
    """Print the report for one state.py.

    Args:
        path: The state.py file.
    """
    with warnings.catch_warnings():
        warnings.simplefilter("error")
        module, state = load(path)
        models = [
            value
            for value in vars(module).values()
            if isinstance(value, type)
            and issubclass(value, BaseModel)
            and value.__module__ == "state"
        ]
        report = {
            "schema": state.model_json_schema(by_alias=False),
            "bad_defaults": bad_defaults(state),
            "aliased": [
                f"{model.__name__}.{name}"
                for model in models
                for name, info in model.model_fields.items()
                if info.alias not in (None, name)
            ],
            "frozen": [
                model.__name__ for model in models if model.model_config.get("frozen")
            ],
        }
    # Not sorted: the order of the properties is the order the class declares.
    print(json.dumps(report))


if __name__ == "__main__":
    main(sys.argv[1])
