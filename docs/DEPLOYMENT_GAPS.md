# Deployment documentation: verified product gaps

Checked against this checkout on 2026-09-15. These are missing package features,
not extra steps developers must add to the native deployment workflow.

| User need | Exact unsupported operation | Evidence | Supported option today |
|---|---|---|---|
| Reuse a separate Python helper across local handlers | Bundle a module imported by a handler without making it an exposed tool | `internal/spec/load.go` loads only named handler files; `internal/generate/{livekit,pipecat}_v1.go` emits one copy per tool name | Put the functions and helper in one authored handler file; each tool gets an independent module copy |
| Ship arbitrary runtime data with local code | Include adjacent JSON, templates, certificates, or an authored module tree | The loader and both generators have no general asset inclusion field | Small constants in the handler; declared knowledge documents use their own inclusion path; otherwise a remote service |
| Use a third-party SDK absent from the generated dependencies | Add a durable package dependency on LiveKit or Pipecat | `internal/spec/package.go:ToolLocal` and `internal/target/table.go:FieldToolDependencies`; both code targets refuse per-tool pins | Use available dependencies or a remote webhook/MCP service; LiveKit target pins only adjust recognized packages |
| Customize worker startup or the image through authored files | Supply a lifecycle hook, custom Dockerfile, or general build override that survives compilation | Target/spec fields and compiler-owned templates; `internal/cli/compile.go:writeArtifactFiles` replaces output | Native settings, supported tool handlers, or host CLI options kept outside the build directory |
| Choose a valid SLNG region in the console | See that deployment region is required when SLNG is selected | The console labels the shared region field optional; SLNG validation refuses omission | Enter a supported region through Advanced target settings before creating the package |
| Deploy every target through Unmute | Run `unmute deploy` for LiveKit or Pipecat | `internal/cli/deploy.go` accepts SLNG only | Use the host CLI from the generated project directory |

SLNG deliberately runs hosted tool references rather than package-local Python.
That target boundary is documented; it is not a request to add a Python runtime.

The dependency refusal text still suggests editing generated `pyproject.toml`
and compiling local tools to SLNG. Those are not durable native alternatives:
compilation replaces the file, and the SLNG target refuses local tool bodies.
The public guide explains the actual limits. Correcting the diagnostic is a
separate code change.

## Verification

- Discovered the public pages through `https://unmute.ai/llms.txt` and read the
  public Variables page and its local source as the layout reference.
- Checked hosting commands against official LiveKit, Pipecat and SLNG docs,
  linked beside the relevant instructions. Checked available installed CLI help
  and source for secret update semantics.
- Validated and compiled the documented reasoning binding and two shared-file
  Python tools for LiveKit and Pipecat in a temporary package. Imported both
  generated handlers and checked ordinary and empty input. Confirmed the source
  filename itself is not copied as an importable shared module.
- Verified the interactive SLNG creation sequence through the real console test harness, then loaded, validated, and generated its output.
- The direct named `init` command could not run on this machine because its existing saved default manifest is invalid. No local configuration was changed. The broader CLI suite also reaches that manifest and its initialization tests fail for the same reason. The model/tool snippets were checked in a separate temporary package.
- A deployment region, organization, key value, model entitlement, and live
  interaction remain destination-specific. Placeholder cloud commands were
  checked but not executed; no account, deployment, or credential was changed.
- `mint validate`, the documentation checks, generated artifact checks, and bundled skill checks pass. The broader CLI suite is limited by the saved manifest described above.
- No provider request or cloud image build was performed. The shared Python
  example uses only the standard library and ran without installing provider SDKs.

See the updated guides under `docs-site/deploy/` and the shared-code section in
`docs-site/build/tools/python.mdx` for user instructions.
