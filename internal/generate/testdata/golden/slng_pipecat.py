# ruff: noqa: F821, F841

def build_front_desk_llm(state=None, *, session_id):
    return _slng_http_service(OpenAIResponsesHttpLLMService(
        api_key=os.environ["SLNG_API_KEY"],
        base_url="https://eu.llm-router.slng.ai/v1",
        settings=OpenAIResponsesLLMSettings(
            model="slng/auto",
            system_instruction=FRONT_DESK_PROMPT,
            extra={**{"reasoning": {"effort": "none"}}, "extra_headers": {"X-Slng-Agent-Id": "router-fixture-v1--agent--front_desk", "X-Slng-Session-Id": session_id}, "extra_body": {"slng_config": {"cache_enabled": True, "tiers": {"1": [{"model": "luna", "weight": 100, "endpoint": {"provider": "openai-responses", "url": "https://api.openai.com/v1", "api_key": os.environ["OPENAI_API_KEY"], "model_id": "gpt-5.6-luna"}}]}}, "template_variables": _slng_snapshot(state, ["caller_name"])}},
        ),
    ))

async def on_activated(self, args) -> None:
    self._slng_owner_template_variables = _slng_snapshot(self.state, ["caller_name"])
    await self.queue_frame(LLMUpdateSettingsFrame(
        delta=self._slng_settings(
            FRONT_DESK_PROMPT,
            "router-fixture-v1--agent--front_desk",
            self._slng_owner_template_variables,
        ),
    ))
    await super().on_activated(args)

async def _slng_task_pre_action(self, action, flow_manager) -> None:
    await self.queue_frame(LLMUpdateSettingsFrame(
        delta=self._slng_settings(
            action["system_instruction"],
            action["scope_id"],
            action["template_variables"],
        ),
    ))

async def _restore_slng_owner(self) -> None:
    await self.queue_frame(LLMUpdateSettingsFrame(
        delta=self._slng_settings(
            FRONT_DESK_PROMPT,
            "router-fixture-v1--agent--front_desk",
            self._slng_owner_template_variables,
        ),
    ))
    await self.flush_pipeline()

def _do_standalone_node_standalone(self) -> NodeConfig:
    return NodeConfig(
        name="standalone",
        role_message="Write a short standalone summary about {{request_topic}}.\n\nReturn only the summary.\n",
        task_messages=[{"role": "developer", "content": "Begin this step."}],
        pre_actions=[{
            "type": "slng_settings",
            "handler": self._slng_task_pre_action,
            "system_instruction": "Write a short standalone summary about {{request_topic}}.\n\nReturn only the summary.\n",
            "scope_id": "router-fixture-v1--task--standalone",
            "template_variables": _slng_snapshot(self.state, ["request_topic"]),
        }],
        functions=[
            FlowsFunctionSchema(
                name="finish_do_standalone_standalone",
                description="Record the result of this step and finish.",
                properties={"summary": {"type": "string"}},
                required=["summary"],
                handler=self._do_standalone_finish_standalone,
            ),
        ],
    )

async def _do_standalone_finish_standalone(self, args, flow_manager):
    self._do_standalone_results["standalone"] = dict(args)
    # then: return — restore the owner's pre-flow context (messages and
    # tools); only the typed results cross back (merge: results, N13).
    messages, tools = self._do_standalone_snapshot
    await self._restore_slng_owner()
    self.context.set_messages(messages + [{
        "role": "developer",
        "content": "Task results: " + json.dumps(self._do_standalone_results) + " Continue with the caller in one short line.",
    }])
    self.context.set_tools(tools)
    return {"status": "ok"}, None

async def run_bot(transport: BaseTransport, runner_args: RunnerArguments) -> None:
    require_env()
    session_id = str(uuid.uuid4())

    runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)
