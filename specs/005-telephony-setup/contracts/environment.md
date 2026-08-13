# Contract: the environment surface

One name removed, zero added. For a LiveKit SIP package with inbound, outbound, warm, and cold (the human-transfer example):

| Name | Before | After | Who supplies it |
|---|---|---|---|
| `SIP_TRUNK_HOSTNAME` | required | required | operator, from the carrier trunk (termination domain) |
| `SIP_AUTH_USERNAME` | required | required | operator, from the carrier credential list |
| `SIP_AUTH_PASSWORD` | required | required | operator, from the carrier credential list |
| `SIP_FROM_NUMBER` | required | required | operator, the trunk's number; also the key `telephony-setup.sh` resolves by |
| `SUPERVISOR_PHONE_NUMBER`, `BILLING_PHONE_NUMBER` | required (env-form destinations) | required | operator |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | required; Cloud injects on deploy | unchanged | platform or operator |
| `REDIS_URL` | always emitted into `.env.example`; grouped under the platform-supplied section and unread on LiveKit Cloud, where the SIP service is managed | unchanged | operator, self-hosted only; left blank for a Cloud deploy |
| `LIVEKIT_SIP_INBOUND_TRUNK` | required for inbound; dev-supplied locally | **gone from every surface** | nobody; the script resolves the ID and nothing at runtime ever read it (specs/003 R8) |

Notes:

- The four carrier names are the author's choice (SCHEMA N33); the table shows the example's names.
- `${UNMUTE_SIP_TRUNK_ID}` appears only inside `sip-dispatch-rule.json` as a substitution token and is not an environment name: it never enters `.env.example`, the compile report, or any required list, and a test asserts that.
- A deployment that still sets the retired name is unaffected; nothing reads it. The README's retirement sentence says it is safe to delete from `.env` and platform secrets. For local runs, `unmute dev --telephony` used to reject a locally-set value with a "supplied by dev itself" error; after the retirement it is simply ignored, which is the correct end state for a name nothing reads.
- The compile report loses the `dev_supplied_environment` field entirely (it existed only for the retired name).
