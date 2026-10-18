"""Read each local tool handler of a package and print its types, as JSON.

unmute runs this through uv, with the pinned Pydantic and unmute-core, on every
validate and compile of a package. It never runs during a call. The
input schema comes from the handler's signature and the output schema from its
return type, read by unmute-core's own local_tool, so Go and the runtime read
the same types.
"""

import importlib.util
import json
import sys
from collections.abc import Callable
from types import ModuleType
from typing import Any

from unmute_core.tools import local_tool


def load_module(name: str, path: str) -> ModuleType:
    """Import one Python file under a module name.

    Args:
        name: The module name to register.
        path: The file.

    Returns:
        The module.
    """
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise SystemExit(f"{path} cannot be loaded as a Python module")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def function(root: str, ref: str) -> Callable[..., Any]:
    """Return the function a `file.py:function` reference names.

    Args:
        root: The package root.
        ref: The reference.

    Returns:
        The function.
    """
    file, _, name = ref.partition(":")
    module_name = file.removesuffix(".py").replace("/", ".")
    module = sys.modules.get(module_name) or load_module(module_name, f"{root}/{file}")
    found = getattr(module, name, None)
    if not callable(found):
        raise SystemExit(f"{file} defines no function {name}")
    return found


def main(root: str, state: str, tools: str) -> None:
    """Print the types of every tool.

    Args:
        root: The package root.
        state: The state.py path, or empty when the package has none.
        tools: A JSON list of {name, handler, precheck}.
    """
    if state:
        load_module("state", state)
    sys.path.insert(0, root)
    report: dict[str, dict[str, Any]] = {}
    for tool in json.loads(tools):
        handler = function(root, tool["handler"])
        precheck = function(root, tool["precheck"]) if tool["precheck"] else None
        try:
            spec = local_tool(handler, precheck=precheck)
        except TypeError as error:
            report[tool["name"]] = {"error": str(error)}
            continue
        report[tool["name"]] = {
            "description": spec.description,
            "input": spec.parameters,
            "output": spec.output.model_json_schema(by_alias=False),
        }
    print(json.dumps({"tools": report}))


if __name__ == "__main__":
    main(*sys.argv[1:4])
