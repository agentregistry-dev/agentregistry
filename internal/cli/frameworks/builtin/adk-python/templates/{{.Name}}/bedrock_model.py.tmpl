"""
Minimal ADK BaseLlm adapter for AWS Bedrock-hosted Anthropic Claude models.

Uses anthropic.AnthropicBedrock, which reads the default boto3 credential
chain — so the same code works locally (via `aws sso login`) and on
AgentCore (via the auto-provisioned workload identity). No API keys.

Scope: text + function-calling. Streaming, extended thinking, and inline
media are intentionally not implemented — they're not needed for typical
tool-using agents, and keeping this file small keeps the template readable.
"""
from __future__ import annotations

import json
import os
from typing import AsyncGenerator, Optional

from anthropic import AnthropicBedrock
from google.adk.models.base_llm import BaseLlm
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai import types


_ADK_TO_ANTHROPIC_ROLE = {"user": "user", "model": "assistant"}


def _parts_to_blocks(parts: list[types.Part]) -> list[dict]:
    """Translate ADK parts into Anthropic content blocks."""
    blocks: list[dict] = []
    for part in parts or []:
        if part.text:
            blocks.append({"type": "text", "text": part.text})
        elif part.function_call:
            blocks.append(
                {
                    "type": "tool_use",
                    "id": part.function_call.id or part.function_call.name,
                    "name": part.function_call.name,
                    "input": dict(part.function_call.args or {}),
                }
            )
        elif part.function_response:
            result = part.function_response.response
            if not isinstance(result, str):
                result = json.dumps(result)
            blocks.append(
                {
                    "type": "tool_result",
                    "tool_use_id": part.function_response.id or part.function_response.name,
                    "content": result,
                }
            )
    return blocks


def _contents_to_messages(contents: list[types.Content]) -> list[dict]:
    messages: list[dict] = []
    for content in contents or []:
        role = _ADK_TO_ANTHROPIC_ROLE.get(content.role or "user", "user")
        blocks = _parts_to_blocks(content.parts or [])
        if blocks:
            messages.append({"role": role, "content": blocks})
    return messages


_JSON_SCHEMA_KEYS = {"type", "properties", "required", "description", "items", "enum",
                     "const", "default", "anyOf", "oneOf", "allOf", "not", "if", "then",
                     "else", "minimum", "maximum", "minLength", "maxLength", "pattern",
                     "minItems", "maxItems", "uniqueItems", "additionalProperties",
                     "$ref", "$defs", "format", "title"}


def _clean_schema(schema: dict) -> dict:
    """Normalize ADK schema output to valid JSON Schema 2020-12."""
    cleaned = {k: v for k, v in schema.items() if k in _JSON_SCHEMA_KEYS}
    if "type" in cleaned and isinstance(cleaned["type"], str):
        cleaned["type"] = cleaned["type"].lower()
    if "properties" in cleaned:
        cleaned["properties"] = {
            k: _clean_schema(v) if isinstance(v, dict) else v
            for k, v in cleaned["properties"].items()
        }
    if "items" in cleaned and isinstance(cleaned["items"], dict):
        cleaned["items"] = _clean_schema(cleaned["items"])
    return cleaned


def _tools_param(llm_request: LlmRequest) -> list[dict]:
    """Extract Anthropic-shaped tool definitions from the request config."""
    cfg_tools = (llm_request.config.tools if llm_request.config else None) or []
    out: list[dict] = []
    for tool in cfg_tools:
        for fn in getattr(tool, "function_declarations", None) or []:
            schema = (
                fn.parameters.model_dump(exclude_none=True)
                if fn.parameters
                else {}
            )
            schema = _clean_schema(schema)
            schema["type"] = "object"
            if "properties" not in schema:
                schema["properties"] = {}
            out.append(
                {
                    "name": fn.name,
                    "description": fn.description or "",
                    "input_schema": schema,
                }
            )
    return out


def _system_param(llm_request: LlmRequest) -> Optional[str]:
    si = getattr(llm_request.config, "system_instruction", None) if llm_request.config else None
    if not si:
        return None
    if isinstance(si, str):
        return si
    if isinstance(si, types.Content):
        return "".join(p.text or "" for p in (si.parts or []))
    if isinstance(si, list):
        chunks: list[str] = []
        for item in si:
            if isinstance(item, str):
                chunks.append(item)
            elif isinstance(item, types.Content):
                chunks.extend(p.text or "" for p in (item.parts or []))
        return "\n".join(chunks)
    return None


def _response_to_llm_response(resp) -> LlmResponse:
    parts: list[types.Part] = []
    for block in resp.content or []:
        if block.type == "text":
            parts.append(types.Part(text=block.text))
        elif block.type == "tool_use":
            parts.append(
                types.Part(
                    function_call=types.FunctionCall(
                        id=block.id,
                        name=block.name,
                        args=dict(block.input or {}),
                    )
                )
            )

    finish_reason = types.FinishReason.STOP
    if resp.stop_reason == "max_tokens":
        finish_reason = types.FinishReason.MAX_TOKENS

    usage = None
    if resp.usage:
        prompt = resp.usage.input_tokens or 0
        candidates = resp.usage.output_tokens or 0
        usage = types.GenerateContentResponseUsageMetadata(
            prompt_token_count=prompt,
            candidates_token_count=candidates,
            total_token_count=prompt + candidates,
        )

    return LlmResponse(
        content=types.Content(role="model", parts=parts),
        finish_reason=finish_reason,
        usage_metadata=usage,
    )


class BedrockClaude(BaseLlm):
    """ADK BaseLlm backed by anthropic.AnthropicBedrock.

    Credentials: the standard boto3 chain (env vars, SSO, container role,
    workload identity). No API keys involved.

    Example:
      model = BedrockClaude(model="us.anthropic.claude-haiku-4-5-20251001-v1:0")
    """

    max_tokens: int = 4096
    region: Optional[str] = None

    model_config = {"arbitrary_types_allowed": True}

    def _client(self) -> AnthropicBedrock:
        region = (
            self.region
            or os.environ.get("AWS_REGION")
            or os.environ.get("AWS_DEFAULT_REGION")
            or "us-east-1"
        )
        return AnthropicBedrock(aws_region=region)

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        # Streaming deliberately not implemented — non-streaming is enough
        # for tool-using demo agents and keeps this adapter small.
        del stream

        kwargs: dict = {
            "model": self.model,
            "max_tokens": self.max_tokens,
            "messages": _contents_to_messages(llm_request.contents),
        }
        system = _system_param(llm_request)
        if system:
            kwargs["system"] = system
        tools = _tools_param(llm_request)
        if tools:
            kwargs["tools"] = tools

        resp = self._client().messages.create(**kwargs)
        yield _response_to_llm_response(resp)
