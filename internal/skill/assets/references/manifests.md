# Manifest

A manifest sets the company rules an agent must follow. Save it once on the
computer, then reuse it when creating agents. It contains rules, not prompts or
tool implementations.

On this page:

- [Quickstart](#quickstart): save a manifest and create an agent
- [What an agent carries](#what-an-agent-carries): the copied contract and its link
- [Example manifest](#example-manifest): one complete example
- [Every key](#every-key): fields, types, accepted values and defaults
- [Validation and limits](#validation-and-limits): what is checked

## Quickstart

```sh
unmute manifest create acme-corp
unmute init my-agent --from-manifest
```

`manifest create` opens a dedicated terminal editor. Choose a section, set its
rules, then review and save. No text editor setup is needed. With no name, the
command asks for one. Local names use letters, digits, hyphens and underscores.
An existing name is never overwritten.

Lists separate rule settings, numbered values, actions and navigation controls.
Apply completes a form; selecting a preset value keeps it immediately.
Add stays selected after each addition: press Enter to add the next value.
New provider and region entries join the draft when their required fields
are complete. Back cancels an unfinished form or incomplete setup and keeps
completed edits. Only **Review and save** writes the file.

Use arrows and Enter in lists, and Tab or Shift+Tab between form fields and
controls. Press **F2** to choose another section and **F1** for help.
Switching sections asks before discarding incomplete input.
Plain prompts use `:sections` and `:help`.

Model roles offer **No restriction** or selected providers with all models or selected model
IDs. Other rule lists also offer **Allow nothing**.
Advanced holds region rules and named tool restrictions.

The first saved manifest becomes the default. Later creations offer to change
the default.

```sh
unmute init another-agent --from-manifest
unmute manifest use acme-corp
```

`--from-manifest` opens a picker, with the default listed first, and then guides
the choices the chosen contract allows. It is the only way a contract reaches a
new package, and it needs a terminal: a coding assistant asks the user to run
it. Plain `unmute init another-agent` writes the ordinary starter package and
reads no saved manifest at all. `manifest use` changes which one the picker and
the `unmute` console offer first.

A package holding a `manifest` file is governed by it.
Read that copy before implementing the use case.
Choose exact approved model IDs when listed. Provider-wide approval still
requires checking target support and the provider's accepted model IDs.
Keep `slng` as the service provider for SLNG models from different makers.
Preserve the contract and its link; explain conflicting requirements instead
of weakening rules. Then validate the completed package and compile it.
Report runtime testing separately.

Run `unmute skill install` after updating the CLI to refresh the bundled workflow.
Review local skill edits before using `--force` to replace them.

Saved files live in the operating system's user config directory, under
`unmute/manifests/<name>/manifest`. `unmute/default-manifest` holds the default
name. Create prints the saved path. Use the same terminal editor for later changes:

```sh
unmute manifest edit acme-corp
```

Saving an edit writes clean YAML and keeps an exact backup beside the original.
The review explains that comments and formatting will be replaced. Unchanged
edits keep the original file. Revision numbers change only when you edit them.
A missing or invalid saved default is an error, never a reason to silently
create an unrestricted agent.

To edit YAML directly, pass `--editor` to create or edit. This uses `VISUAL`,
falling back to `EDITOR`, such as `EDITOR='code --wait'`. Invalid drafts are
kept at the path in the error. `manifest edit acme-corp --editor` can also
repair an invalid saved file.

## What an agent carries

Initialization copies the chosen file into the new package as `manifest` and
links it from `agent.yaml`:

```yaml
manifest: manifest
```

Commit both files. Validation and compilation read the package's copy, so they
work on another computer and in CI without the local library. Changing the
saved default or editing the saved source does not change existing packages.
To update one, replace its `manifest`, then run `unmute validate` and
`unmute compile`. Do not remove a rule to make an agent pass without the
author's instruction to change the company contract.

A root manifest requires its link. A link requires that file. Only the literal
link `manifest: manifest` is supported; no parent paths or remote URLs. Packages
with neither file nor link continue to work without a company contract.

## Example manifest

```yaml manifest
manifest: acme-corp
version: 1

models:
  listen:
    - provider: slng
      allow:
        - deepgram/nova:3
  speak:
    - provider: slng
      allow:
        - deepgram/aura:2
  think:
    - provider: openai
      allow:
        - gpt-5.6-terra

languages:
  allow:
    - en

regions:
  models:
    - role: listen
      provider: slng
      allow:
        - eu-north
    - role: speak
      provider: slng
      allow:
        - eu-north
  deployments:
    - provider: livekit
      allow:
        - eu-central

targets:
  allow:
    - livekit

tools:
  kinds:
    allow:
      - builtin
  names:
    allow:
      - end_call
  builtin:
    allow:
      - end_call

tracing:
  allow:
    - langfuse
    - coval
```

## Every key

Only `manifest` and `version` are required. Add the rule blocks you need.
Omitting a rule adds no restriction. An explicit empty allowlist permits
nothing. Lists cannot contain `null`, blank strings or duplicate entries, and
unknown keys are refused.

### Identity

#### `manifest`

Type: `string`. Required within its block.

The name of the organization this manifest describes, such as `acme-corp`.
Any non-blank text is accepted. This is separate from the local name chosen
with `unmute manifest create`, which identifies the saved file.

#### `version`

Type: `integer`. Required within its block.

The revision of this manifest: any integer greater than or equal to `1`.
Increase it when you publish changed rules. It does not select the agent
schema, an Unmute release, or a LiveKit or Pipecat SDK version.

### Models

A **provider** is the service the agent connects to. A **model maker** creates
the model that service offers. For models served through SLNG, keep the
provider as `slng`, even when their IDs name Deepgram, Cartesia or Soniox.

Each role can allow several providers, and each provider can allow several
models. **Add provider** opens the service picker, then offers **Allow all models**
or **Add model ID**. Adding a model opens the input directly.
Use **Add model ID** again for more models, or **Add provider** for another service. Completed entries stay in your draft as you move between
screens; no extra Apply step is needed.

The guided editor does not offer **Allow nothing** for models.
Existing empty model rules remain visible and must be repaired before saving.
Add models, choose **Allow all models** for a provider, or choose **No restriction** for the role.
The YAML format and `--editor` still support explicitly empty lists.

#### `models`

Type: `object`. Optional.

Restricts model choices by role. Its only keys are `listen`, `speak` and
`think`. Each role contains a list of provider entries. Agents still define
their own model profiles; the manifest approves exact provider/model pairs.
Turn models are outside these rules.

#### `models.listen`

Type: `object[]`. Optional.

Allowed speech-to-text (STT) providers and models. Omit it for no STT
restriction, or write `listen: []` to allow no STT models. When present,
providers missing from this list are forbidden for this role. A provider
may appear only once.

#### `models.listen[].provider`

Type: `string`. Required within its block.

The exact provider value used by the agent's listen profile, such as `slng`
or `deepgram`. See [STT providers](https://unmute.ai/models/stt) for supported integrations
on each target. Provider names are strings, not a fixed manifest enum.

#### `models.listen[].allow`

Type: `string[]`. Optional.

Omit this field to allow all current and future models from this provider.
Other providers remain forbidden unless listed for the role.

Exact model IDs approved for this provider, such as `deepgram/nova:3` with
`slng`. Model IDs are provider-defined strings. No wildcards or prefix
matching; case must match. An empty list approves none of this provider's
STT models. There is no default model.

#### `models.speak`

Type: `object[]`. Optional.

Allowed text-to-speech (TTS) providers and models. Omit it for no TTS
restriction, or write `speak: []` to allow no TTS models. Providers not
listed are forbidden for this role. A provider may appear only once.

#### `models.speak[].provider`

Type: `string`. Required within its block.

The exact provider value used by the agent's speak profile, such as `slng`
or `elevenlabs`. See [TTS providers](https://unmute.ai/models/tts) for target support.
Provider names are strings, not a fixed manifest enum.

#### `models.speak[].allow`

Type: `string[]`. Optional.

Omit this field to allow all current and future models from this provider.
Other providers remain forbidden unless listed for the role.

Exact model IDs approved for this provider, such as `deepgram/aura:2` with
`slng`. IDs are provider-defined and case-sensitive; wildcards are not
supported. An empty list approves no models. This does not restrict voice
IDs or supply a default voice.

#### `models.think`

Type: `object[]`. Optional.

Allowed language-model (LLM) providers and models. Omit it for no LLM
restriction, or write `think: []` to allow no LLM models. Providers not
listed are forbidden for this role. A provider may appear only once.

#### `models.think[].provider`

Type: `string`. Required within its block.

The exact provider value used by the agent's think profile, such as
`openai` or `slng`. See [LLM providers](https://unmute.ai/models/llm) for target support.
Match the configured provider, including when it routes to another service.
Provider names are strings, not a fixed manifest enum.

#### `models.think[].allow`

Type: `string[]`. Optional.

Omit this field to allow all current and future models from this provider.
Other providers remain forbidden unless listed for the role.

Exact model IDs approved for this provider, such as `gpt-5.6-terra` with
`openai`. IDs are provider-defined and case-sensitive. No wildcards or automatic model choice. An empty list approves
no models.

### Languages

#### `languages`

Type: `object`. Optional.

Limits configured STT and TTS languages. Its only key is `allow`. Omitting
this block adds no language restriction. It changes no LLM prompt and
does not guarantee the language of every spoken word.

#### `languages.allow`

Type: `string[]`. Required within its block.

Language tags such as `en`, `es` or `en-US`. Values are not a fixed enum:
the accepted shape is 2 to 8 letters, followed by zero or more hyphen-separated
groups of 1 to 8 letters or digits. The speech integration must also support
the configured tag.

Matching ignores case, but uses the whole tag: `en` does not approve
`en-US`. No language is chosen by omission. An empty list forbids all
speech languages, including an unset one. For a nonempty list, automatic
or hidden language settings that cannot be checked produce a warning.

### Regions

#### `regions`

Type: `object`. Optional.

Limits model-service and deployment regions separately. Its only keys are
`models` and `deployments`. Neither list sets the other location.

#### `regions.models`

Type: `object[]`. Optional.

Region rules for model services, matched by `role` and `provider`. Each
pair may appear only once. An omitted or empty rule list adds no model
region restrictions; a rule's own empty `allow` list forbids that pair.
Providers and roles with no matching row have no added region rule.

#### `regions.models[].role`

Type: `listen | speak | think`. Required within its block.

Exactly one of `listen` (STT), `speak` (TTS), or `think` (LLM).
There is no default and no `turn` option.

#### `regions.models[].provider`

Type: `string`. Required within its block.

The exact configured model provider this row governs, such as `slng`
or `aws`. This is a model provider, not the deployment target. It must be
nonempty; provider names are not a fixed manifest enum.

#### `regions.models[].allow`

Type: `string[]`. Required within its block.

Exact native region names for the matched service. There are no shared
`EU` aliases, wildcards or automatic geographic conversions. Matching is
case-sensitive. An empty list forbids this provider/role even when its
region is unknown. A nonempty list warns when the region cannot be checked.

The compiler currently reads these settings:

| Service | Agent setting | Region values |
|---|---|---|
| SLNG listen/speak/think on code targets | `params.world_part` | `us-east`, `us-west`, `br`, `eu-west`, `eu-north`, `gb`, `za`, `il`, `jp`, `sg`, `id`, `in`, `au` |
| AWS think on LiveKit | `params.region` | Native AWS region strings; there is no closed list in Unmute |

Other providers, unsupported settings and endpoints supplied through
environment variables do not establish a verifiable model region.
A checked gateway setting is not proof of the upstream processing location.

#### `regions.deployments`

Type: `object[]`. Optional.

Region rules matched by deployment provider. Each provider may appear only
once. Omitting this list or a provider's row adds no region restriction
for that provider.

#### `regions.deployments[].provider`

Type: `livekit | pipecat | slng`. Required within its block.

Exactly one of `livekit`, `pipecat`, or `slng`. Match the target's
`provider`, not its instance name. There is no default.

#### `regions.deployments[].allow`

Type: `string[]`. Required within its block.

Exact native values permitted in the target's `deployment_region`.
If the target declares several regions, every region must be allowed.
An empty list forbids deployment on this provider, including when no
region is declared. With a nonempty list, an omitted region warns.

LiveKit and Pipecat use their platform's region strings; Unmute does not
maintain a closed list. SLNG uses the same 13 regions as its model services:
  `us-east`, `us-west`, `br`, `eu-west`, `eu-north`, `gb`, `za`, `il`, `jp`, `sg`, `id`, `in`, `au`. The retired `any` value is refused. See [Targets](https://unmute.ai/reference/targets-yaml).

### Deployment targets

#### `targets`

Type: `object`. Optional.

Restricts where the package can be compiled or deployed. Its only key is
`allow`. Omitting it adds no target restriction.

#### `targets.allow`

Type: `string[]`. Required within its block.

Any subset of `livekit`, `pipecat`, and `slng`. These are deployment
providers, not target instance names. An empty list allows no target.
Every target declared in the package is checked, even when a command
selects only one of them.

### Tools

#### `tools`

Type: `object`. Optional.

Restricts tools by kind and identity. The available keys are `kinds`,
`names`, `builtin`, and `slng`. Every applicable rule must pass.
Permission does not create or attach a tool, and it does not add target
support for that tool.

#### `tools.kinds`

Type: `object`. Optional.

Restricts execution kinds. Its only key is `allow`. Omit it to leave
execution kinds unrestricted by the manifest.

#### `tools.kinds.allow`

Type: `string[]`. Required within its block.

Any subset of these eight values:

| Value | Tool kind |
|---|---|
| `webhook` | An HTTP tool |
| `local` | A local Python handler |
| `mcp` | A tool provided by an MCP server |
| `builtin` | A built-in tool ID |
| `client` | A client-side tool declaration; currently gated on all targets |
| `provider_hosted` | A provider-hosted declaration; currently gated on all targets |
| `knowledge` | A knowledge lookup |
| `slng` | A named tool hosted on SLNG |

An empty list allows no tools. The ordinary target capability checks still
apply; listing a gated kind here does not enable it.

#### `tools.names`

Type: `object`. Optional.

Restricts package tool names. Its only key is `allow`. Omission adds no
package-name restriction.

#### `tools.names.allow`

Type: `string[]`. Required within its block.

Exact names from the package's `tools` list: for example, `check_booking`
means `tools/check_booking.yaml`. Names are user-defined strings, with no
wildcard matching. An empty list permits no package tools.

#### `tools.builtin`

Type: `object`. Optional.

Restricts builtin IDs in addition to any kind or package-name rules.
Its only key is `allow`. This rule applies only to builtin tools.

#### `tools.builtin.allow`

Type: `string[]`. Required within its block.

Exact builtin IDs. Currently `end_call` is supported on all three targets;
`send_sms` is supported only on SLNG. The manifest accepts nonempty ID
strings, while tool validation checks whether the ID and target are
supported. An empty list forbids builtin tools.

#### `tools.slng`

Type: `object`. Optional.

Restricts the identity of tools hosted on SLNG. Its only key is `allow`.
This rule applies to a `slng:` tool on any target that uses it.

#### `tools.slng.allow`

Type: `string[]`. Required within its block.

Exact hosted tool names from `slng:`, which may differ from the local tool
filename. Names are organization-defined strings. There is no wildcard or
automatic discovery. The legacy pinned form uses the local package tool
name. An empty list forbids SLNG-hosted tools.

### Tracing

#### `tracing`

Type: `object`. Optional.

Restricts the tracing provider when tracing is enabled. Its only key is
`allow`. Tracing stays optional; this block does not turn it on or require it.

#### `tracing.allow`

Type: `string[]`. Required within its block.

Any subset of `langfuse` and `coval`. An empty list requires tracing to
remain disabled. Omitting this block adds no provider restriction.
Target support and credential requirements still apply.

## Validation and limits

The contract covers the whole declared package, including unused profiles,
fallbacks and every target override. Selecting one target for compilation does
not hide a violation in another target. Known violations fail before output is
written. Automatic or hidden language and region values that cannot be checked
produce warnings. Read those warnings; a successful compile does not prove
data residency. The compile report records the contract name, revision and
verification warnings.

Existing target capability checks still apply. Permission in a manifest does
not add provider support or make an unsupported setting work.

The creation console cannot collect custom model endpoint settings or a SLNG
Context Router upstream. It excludes those model choices. If your contract
allows only such bindings, create the package with plain `unmute init`, copy the
saved manifest into it as `manifest`, name that file in `agent.yaml`, and write
the bindings by hand or with a coding assistant. Validation and compilation
still enforce the same rules.

The contract checks declared settings, not arbitrary local tool code, the
provider's actual processing location or tracing delivery. It is not signed
and does not prevent somebody deleting both file and link. Pronunciation
dictionaries, compliance libraries, placement, URL restrictions, dependency
rules and prefetch limits are not part of this version.
