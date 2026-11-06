#!/usr/bin/env python3
"""Apply the Gemma current-tool-cycle fix to the exact pinned oMLX wheel.

The upstream wheel's native kernels are retained byte-for-byte. The wheel is
an artifact of v0.7.4.dev1, whose tag resolves to the source SHA in inputs.json.
No kernel ABI, dependencies, template or model weights are changed here.
"""
import argparse
import base64
import csv
import hashlib
import io
import json
import pathlib
import zipfile


def patched_source(source):
    original = '''    i = 0
    while i < len(raw):'''
    replacement = '''    # Gemma's official template retains reasoning only within the active
    # tool cycle. A new real user message closes the previous cycle.
    last_user_index = max(
        (index for index, message in enumerate(raw) if message.get("role") == "user"),
        default=-1,
    )

    i = 0
    while i < len(raw):'''
    if source.count(original) != 1:
        raise ValueError("Pinned oMLX message-loop context changed")
    source = source.replace(original, replacement)
    original = '''            # Per Gemma 4's multi-turn rule, prior thought blocks must not
            # be fed back into the next turn. Strip them before rendering.
            content = _strip_thinking(content)
            content = _strip_protocol_markers(content)

            out_msg: dict = {"role": "assistant", "content": content or ""}
'''
    replacement = '''            # The pinned Gemma templates consume reasoning_content for
            # assistant tool calls after the last user. Preserve that field
            # before stripping visible content; completed past turns omit it.
            reasoning = None
            if tool_calls_raw and i > last_user_index:
                reasoning = msg.get("reasoning") or msg.get("reasoning_content")
                if not reasoning and isinstance(content, str):
                    leading = _LEADING_THOUGHT_RE.match(content)
                    if leading:
                        spans = re.findall(
                            r"<think>(.*?)</think>|<\\|channel>thought\\n(.*?)<channel\\|>",
                            leading.group(0),
                            re.DOTALL,
                        )
                        reasoning = "\\n".join(
                            (rendered or native).strip() for rendered, native in spans
                        )
            content = _strip_thinking(content)
            content = _strip_protocol_markers(content)

            out_msg: dict = {"role": "assistant", "content": content or ""}
            if reasoning:
                if not isinstance(reasoning, str):
                    raise ValueError("Gemma tool-cycle reasoning must be text")
                out_msg["reasoning_content"] = reasoning
'''
    if source.count(original) != 1:
        raise ValueError("Pinned oMLX thinking-extraction context changed")
    return source.replace(original, replacement)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=pathlib.Path)
    parser.add_argument("destination", type=pathlib.Path)
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    pinned = json.loads((root / "packaging/runtimes/inputs.json").read_text())["omlx"]
    if hashlib.sha256(args.source.read_bytes()).hexdigest() != pinned["sha256"]:
        raise ValueError("Upstream oMLX wheel SHA-256 does not match the pinned input")
    with zipfile.ZipFile(args.source) as wheel:
        contents = {name: wheel.read(name) for name in wheel.namelist() if not name.endswith("/")}
    source_path = "omlx/adapter/gemma4.py"
    contents[source_path] = patched_source(contents[source_path].decode()).encode()
    patch_digest = hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
    contents["omlx/_gemma_build.json"] = json.dumps({"sourceSha": pinned["sourceSha"], "upstreamWheelSha256": pinned["sha256"], "patchSha256": patch_digest}, sort_keys=True).encode()
    records = [name for name in contents if name.endswith(".dist-info/RECORD")]
    if len(records) != 1:
        raise ValueError("Expected one wheel RECORD")
    record = records[0]
    output = io.StringIO(newline="")
    writer = csv.writer(output, lineterminator="\n")
    for name, data in sorted(contents.items()):
        if name != record:
            digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip("=")
            writer.writerow([name, "sha256=" + digest, len(data)])
    writer.writerow([record, "", ""])
    contents[record] = output.getvalue().encode()
    args.destination.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(args.destination, "w", compression=zipfile.ZIP_DEFLATED) as wheel:
        for name, data in sorted(contents.items()):
            wheel.writestr(name, data)
    print(json.dumps({"wheel": str(args.destination), "sha256": hashlib.sha256(args.destination.read_bytes()).hexdigest(), "patchSha256": patch_digest}))


if __name__ == "__main__":
    main()
