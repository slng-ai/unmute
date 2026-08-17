# ruff: noqa: F821

FRONT_DESK_PROMPT = """You are the front desk assistant for {{caller_name}}.

Help with the request, delegate a standalone summary when useful, and transfer
specialist questions to the specialist agent.
"""

def _slng_snapshot(state, names: list[str]) -> dict[str, str]:
    """Freeze the referenced prompt values without exposing oversized values."""
    snapshot = {}
    for name in names:
        value = getattr(state, name, None)
        text = "" if value is None else str(value)
        if len(text) > 4000:
            raise RuntimeError(f"Template variable {name} exceeds 4000 characters")
        snapshot[name] = text
    return snapshot

_SLNG_SESSION_ID: ContextVar[str | None] = ContextVar(
    "_slng_session_id", default=None
)
_SLNG_ACTIVE_SCOPE: ContextVar[
    list[tuple[str, str, dict[str, str]] | None] | None
] = ContextVar(
    "_slng_active_scope", default=None
)


def _activate_slng_scope(
    agent_id: str,
    prompt: str,
    template_variables: dict[str, str],
) -> None:
    scope = (agent_id, prompt, template_variables)
    carrier = _SLNG_ACTIVE_SCOPE.get()
    if carrier is None:
        _SLNG_ACTIVE_SCOPE.set([scope])
    else:
        carrier[0] = scope


def _current_slng_scope() -> tuple[str, str, dict[str, str]]:
    carrier = _SLNG_ACTIVE_SCOPE.get()
    if carrier is None or carrier[0] is None:
        raise RuntimeError("SLNG prompt scope is unavailable outside an active call")
    return carrier[0]


def _slng_prompt_context(chat_ctx, prompt: str):
    """Keep raw placeholders only on the SLNG request's agent instructions."""
    request_ctx = chat_ctx.copy()
    index = request_ctx.index_by_id("lk.agent_task.instructions")
    if index is not None:
        instruction = request_ctx.items[index]
        if instruction.type != "message":
            raise RuntimeError("LiveKit instruction item is not a message")
        request_ctx.items[index] = instruction.model_copy(update={"content": [prompt]})
    return request_ctx

class FrontDesk(Agent):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN, initial: bool = False) -> None:
        self._initial = initial
        super().__init__(
            instructions=FRONT_DESK_PROMPT,
            chat_ctx=chat_ctx,
        )

    async def on_enter(self) -> None:
        _activate_slng_scope(
            "router-fixture-v1--agent--front_desk",
            FRONT_DESK_PROMPT,
            _slng_snapshot(self.session.userdata, ["caller_name"]),
        )
        if not self._initial:
            self.session.generate_reply()
            return
        await self.session.say("Hello. How can I help?")

async def do_standalone(self, ctx: RunContext) -> dict:
    """The caller asks for a short standalone summary. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request."""
    # N13: snapshot before the task, restore after. An awaited AgentTask
    # merges its own turns into this agent's context when it returns
    # (livekit/agents/voice/agent.py, merge on handoff-return), so without
    # this the follow-up prompt ends on the task's last assistant line with
    # no tool record of the work. That reads as unfinished, and the model
    # runs the same flow again (B: multi-task delegated twice and booked
    # nothing, 2026-08-15). The task-group branch below always did this.
    owner_ctx = self.chat_ctx.copy()
    owner_slng_scope = _current_slng_scope()
    result = await Standalone(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))
    _activate_slng_scope(*owner_slng_scope)
    await self.update_chat_ctx(owner_ctx)
    # C4/N13: the task's turns are not propagated back; the typed result is
    # the only return.
    return result

class Standalone(AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=STANDALONE_PROMPT, chat_ctx=chat_ctx)

    async def on_enter(self) -> None:
        _activate_slng_scope(
            "router-fixture-v1--task--standalone",
            STANDALONE_PROMPT,
            _slng_snapshot(self.session.userdata, ["request_topic"]),
        )
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

class _SLNGResponsesLLM(openai.responses.LLM):
    """Add the compiler-owned SLNG request fields to every Responses call."""

    def __init__(
        self,
        *,
        slng_config,
        **kwargs,
    ):
        super().__init__(**kwargs)
        self._slng_config = slng_config

    def chat(self, *, chat_ctx, extra_kwargs=NOT_GIVEN, **kwargs):
        session_id = _SLNG_SESSION_ID.get()
        carrier = _SLNG_ACTIVE_SCOPE.get()
        scope = None if carrier is None else carrier[0]
        if session_id is None or scope is None:
            raise RuntimeError("SLNG request identity is unavailable outside an active call scope")
        agent_id, prompt, template_variables = scope
        request_extra = dict(extra_kwargs) if isinstance(extra_kwargs, dict) else {}
        extra_headers = dict(request_extra.get("extra_headers", {}))
        extra_headers.update(
            {
                "X-Slng-Agent-Id": agent_id,
                "X-Slng-Session-Id": session_id,
            }
        )
        extra_body = dict(request_extra.get("extra_body", {}))
        extra_body.update(
            {
                "slng_config": self._slng_config,
                "template_variables": template_variables,
            }
        )
        request_extra["extra_headers"] = extra_headers
        request_extra["extra_body"] = extra_body
        stream = super().chat(
            chat_ctx=_slng_prompt_context(chat_ctx, prompt),
            extra_kwargs=request_extra,
            **kwargs,
        )
        process_event = stream._process_event

        def process_slng_event(event):
            if getattr(event, "type", None) == "response.incomplete":
                details = getattr(event.response, "incomplete_details", None)
                reason = getattr(details, "reason", None) or "unknown reason"
                raise APIStatusError(
                    f"response.incomplete: {reason}",
                    status_code=-1,
                    retryable=False,
                )
            return process_event(event)

        setattr(stream, "_process_event", process_slng_event)
        return stream

async def entrypoint(ctx: JobContext) -> None:
    require_env()
    _SLNG_SESSION_ID.set(str(uuid.uuid4()))
    _SLNG_ACTIVE_SCOPE.set([None])

llm=_SLNGResponsesLLM(api_key=os.environ["SLNG_API_KEY"], model="slng/auto", base_url="https://eu.llm-router.slng.ai/v1", use_websocket=False, store=False, slng_config={"cache_enabled": True, "tiers": {"1": [{"model": "luna", "weight": 100, "endpoint": {"provider": "openai-responses", "url": "https://api.openai.com/v1", "api_key": os.environ["OPENAI_API_KEY"], "model_id": "gpt-5.6-luna"}}]}}),
