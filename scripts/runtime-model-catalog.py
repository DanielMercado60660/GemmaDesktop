#!/usr/bin/env python3
"""Pin model metadata/small files and generate acquisition manifests, no weights."""
import hashlib
import json
import pathlib
import urllib.parse
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
pins = json.loads((ROOT / "packaging/runtimes/inputs.json").read_text())["models"]
resources = ROOT / "dist/runtime-resources"
resources.mkdir(parents=True, exist_ok=True)
models = []
redirect_hosts = ["huggingface.co", "cdn-lfs.huggingface.co", "cdn-lfs-us-1.hf.co", "cas-bridge.xethub.hf.co", "us.aws.cdn.hf.co", "us-east-1.aws.cdn.hf.co"]
for pin in pins:
    repository, revision = pin["repository"], pin["revision"]
    with urllib.request.urlopen(f"https://huggingface.co/api/models/{repository}/revision/{revision}?blobs=true", timeout=30) as response:
        metadata = json.load(response)
    if metadata["sha"] != revision:
        raise ValueError("Model revision did not resolve exactly")
    files = []
    small = {}
    for entry in sorted(metadata["siblings"], key=lambda item: item["rfilename"]):
        name = entry["rfilename"]
        if name == ".gitattributes":
            continue
        if not name.endswith((".json", ".jinja", ".safetensors", ".md")):
            raise ValueError("Unexpected model artifact type: " + name)
        url = f"https://huggingface.co/{repository}/resolve/{revision}/{urllib.parse.quote(name)}"
        if "lfs" in entry:
            digest = entry["lfs"]["sha256"]
        else:
            if entry["size"] > 1 << 20:
                raise ValueError("Non-LFS artifact is unexpectedly large")
            with urllib.request.urlopen(url, timeout=30) as response:
                data = response.read((1 << 20) + 1)
            if len(data) != entry["size"]:
                raise ValueError("Small model artifact size changed")
            # Verify Git object identity as well as recording content SHA-256.
            git_digest = hashlib.sha1(f"blob {len(data)}\0".encode() + data).hexdigest()
            if git_digest != entry["blobId"]:
                raise ValueError("Small artifact Git identity mismatch")
            digest = hashlib.sha256(data).hexdigest()
            small[name] = data
        source = {"url": url, "redirectHosts": redirect_hosts, "sha256": digest, "size": entry["size"]}
        files.append({"path": name, "sha256": digest, "size": entry["size"], "source": source})
    by_name = {file["path"]: file for file in files}
    if by_name["chat_template.jinja"]["sha256"] != pin["templateSha256"]:
        raise ValueError("Pinned model template changed")
    configuration = json.loads(small["config.json"])
    inventory = [{key: file[key] for key in ("path", "sha256", "size")} for file in files]
    artifact_digest = hashlib.sha256(json.dumps(inventory, separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()
    model = {"name": pin["id"], "artifactDigest": "sha256:" + artifact_digest, "tokenizerDigest": "sha256:" + by_name["tokenizer.json"]["sha256"], "templateDigest": "sha256:" + pin["templateSha256"], "architecture": configuration["model_type"], "contextTokens": configuration.get("text_config", configuration)["max_position_embeddings"], "capabilities": {"completion": True, "tools": True, "thinking": True, "vision": False, "structuredOutput": False}}
    model["capabilityClaims"] = {capability: {"state": "unknown", "advertised": capability in ("completion", "tools", "thinking", "vision"), "source": "pinned_model_configuration_and_template", "evidenceDigest": "sha256:" + pin["templateSha256"]} for capability in ("completion", "tools", "thinking", "vision", "structuredOutput")}
    license_name = (metadata.get("cardData") or {}).get("license") or "See the original Gemma 4 license and pinned repository README"
    manifest = {"version": 1, "id": pin["id"], "repository": repository, "revision": revision, "serveName": pin["id"], "license": license_name, "providers": [], "model": model, "files": files}
    models.append(manifest)
catalog_path = resources / "catalog.json"
catalog = json.loads(catalog_path.read_text()) if catalog_path.exists() else {"version": 1, "runtimes": [], "models": []}
catalog["models"] = models
catalog_path.write_text(json.dumps(catalog, indent=2) + "\n")
(ROOT / "packaging/runtimes/model-manifests.json").write_text(json.dumps({"version": 1, "models": models}, indent=2) + "\n")
for model in models:
    print(json.dumps({"id": model["id"], "revision": model["revision"], "artifactDigest": model["model"]["artifactDigest"], "bytes": sum(file["size"] for file in model["files"]), "providerQualification": "unknown; explicit bounded check required"}))
