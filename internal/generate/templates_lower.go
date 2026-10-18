package generate

// This file lowers the variable-template surface for both code drivers
// (variable_secrets_specs.md T6/T7). Each driver reads its call state through a
// different expression — Pipecat a `state` dataclass, LiveKit `ctx.userdata` —
// so every helper takes that accessor rather than hard-coding one.
