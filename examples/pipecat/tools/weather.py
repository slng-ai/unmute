from pipecat.adapters.schemas.function_schema import FunctionSchema
from pipecat.services.llm_service import FunctionCallParams

# Explicit tool schema. On pipecat 1.3.0 the `tools` arg only accepts a
# ToolsSchema, and "direct" functions (params + required named args) cannot
# satisfy the DirectFunction protocol under `ty`, so we advertise the schema
# here and register the handler separately via llm.register_function below.
WEATHER_SCHEMA = FunctionSchema(
	name="get_current_weather",
	description="Get the current weather.",
	properties={
		"location": {
			"type": "string",
			"description": 'The city and state, e.g. "San Francisco, CA".',
		},
		"format": {
			"type": "string",
			"enum": ["celsius", "fahrenheit"],
			"description": "The temperature unit to use.",
		},
	},
	required=["location", "format"],
)


async def get_current_weather(params: FunctionCallParams):
	"""Handle a get_current_weather call. The LLM args arrive in params.arguments."""
	# location = params.arguments["location"]
	# temperature_format = params.arguments["format"]
	await params.result_callback({"conditions": "sunny", "temperature": "31"})
