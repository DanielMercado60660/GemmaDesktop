#!/usr/bin/env python3
"""Offline regression checks against pinned Gemma model templates (no weights)."""
import hashlib
import json
import pathlib
import sys

from jinja2.sandbox import ImmutableSandboxedEnvironment
from omlx.adapter.gemma4 import extract_gemma4_messages


def check(condition, message):
    if not condition:
        raise AssertionError(message)


root = pathlib.Path(__file__).resolve().parent.parent
tools = [{"type": "function", "function": {"name": "inspect", "description": "Read a path", "parameters": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}}]
call = {"id": "stable-id", "type": "function", "function": {"name": "inspect", "arguments": '{"path":"a"}'}}
history = [
    {"role": "user", "content": "old request"},
    {"role": "assistant", "content": "", "reasoning_content": "OLD THOUGHT", "tool_calls": [dict(call, id="old-id")]},
    {"role": "tool", "tool_call_id": "old-id", "content": '{"result":"old"}'},
    {"role": "assistant", "content": "old answer"},
    {"role": "user", "content": "current request"},
    {"role": "assistant", "content": "", "reasoning_content": "CURRENT THOUGHT", "tool_calls": [call]},
    {"role": "tool", "tool_call_id": "stable-id", "content": '{"result":"current"}'},
]
processed = extract_gemma4_messages(history, consolidate_system_messages=False)
check("reasoning_content" not in processed[1], "past tool-cycle reasoning leaked")
check(processed[-1]["reasoning_content"] == "CURRENT THOUGHT", "current tool-cycle reasoning lost")
check(processed[-1]["tool_calls"][0]["id"] == "stable-id", "tool call ID changed")
check(processed[-1]["tool_calls"][0]["function"]["arguments"] == {"path": "a"}, "tool arguments changed")
check(processed[-1]["tool_responses"][0] == {"name": "inspect", "response": {"result": "current"}}, "tool result association changed")

for embedded in ("<think>\nCURRENT THOUGHT\n</think>\n", "<|channel>thought\nCURRENT THOUGHT\n<channel|>"):
    replay = history[:-2] + [{"role": "assistant", "content": embedded, "tool_calls": [call]}, history[-1]]
    parsed = extract_gemma4_messages(replay, consolidate_system_messages=False)
    check(parsed[-1]["reasoning_content"] == "CURRENT THOUGHT", "embedded current thought was dropped")
    check(parsed[-1]["content"] == "", "embedded thought duplicated into visible content")

env = ImmutableSandboxedEnvironment(trim_blocks=True, lstrip_blocks=True)
env.globals["raise_exception"] = lambda value: (_ for _ in ()).throw(ValueError(value))
evidence = []
for name in ("gemma4-e4b", "gemma4-12b-26b"):
    template_path = root / "packaging/runtimes/templates" / (name + ".jinja")
    source = template_path.read_bytes()
    template = env.from_string(source.decode())
    rendered = template.render(messages=processed, tools=tools, enable_thinking=True, add_generation_prompt=True, bos_token="<bos>")
    check("OLD THOUGHT" not in rendered, name + ": old reasoning leaked into prompt")
    check(rendered.count("CURRENT THOUGHT") == 1, name + ": current reasoning missing or duplicated")
    check("<|channel>thought\nCURRENT THOUGHT\n<channel|>" in rendered, name + ": wrong thought protocol")
    check("<|tool_response>" in rendered and "current" in rendered, name + ": tool response lost")
    off = template.render(messages=[{"role": "user", "content": "hello"}], tools=[], enable_thinking=False, add_generation_prompt=True, bos_token="<bos>")
    empty_thought = "<|channel>thought\n<channel|>"
    check((empty_thought in off) == (name == "gemma4-12b-26b"), name + ": thinking-off template mismatch")
    evidence.append({"template": name, "sha256": hashlib.sha256(source).hexdigest(), "passed": True})
print(json.dumps({"checks": "current/past cycle, raw spans, tool IDs/arguments/results, official template rendering, model-specific thinking-off", "templates": evidence}))
