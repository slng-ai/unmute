package generate

// LocalRunEnv marks a run as a local `unmute dev` one, so the emitted tracing
// module can label its traces apart from the same build running in the cloud.
// No deploy path sets it.
//
// This constant is the single owner of the name. It lives here, next to the
// template that reads it, because `unmute dev` sets it and the telemetry
// template hardcodes the same string; `TestCovalTracingOwnsTheLocalRunMarker`
// fails if the two ever disagree.
const LocalRunEnv = "UNMUTE_LOCAL_RUN"
