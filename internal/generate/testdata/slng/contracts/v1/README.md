# Shared-tool contract v1

These schemas freeze the target compiler boundary. They do not activate shared-tool
execution. Until R03 replaces the operational LiveKit `JobConfig` parser, registry,
MCP, secret-value, and broken-reference side channels remain legacy-only compatibility
inputs. A shared agent must fail closed instead of treating those side channels as proof
that the strict target runtime contract is enforced.

The JSON fixtures, model-schema hashes, and policy constants in this directory are
cross-repository conformance artifacts and must remain byte-identical.
