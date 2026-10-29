# Local video retrieval: pinned design and qualification plan

Research date: 2026-10-27. **Design only: no model download, dependency installation, media execution, embedding inference, or retrieval/answer qualification was performed for this report.** The coding qualification remains a separate prerequisite. Source inspection and package metadata establish plausible compatibility, not a working product.

> **Runtime update — 2026-10-27:** the user upgraded Ollama from **0.32.3 to 0.40.11**. The earlier text-only embedding limitation and Gemma4 MLX vision/audio suppression below are **historical 0.32.3 findings**, not current limitations. Pinned 0.40.11 source adds text/image/audio embeddings and Gemma4 MLX media handling; the captured local `/api/show` advertises vision for the installed 26B and vision/audio for the 12B. **No image/audio/embedding or video-RAG acceptance test has been performed for this update.** Video-RAG remains conditional on coding qualification and its separate media tests. A separate GGUF answer-model download is no longer a demonstrated prerequisite; do not recommend or initiate it before testing the already-installed candidates. See the source addendum below. [Captured local metadata](../evidence/ollama040-model-metadata.json)

## Recommendation and compatibility

The original 0.32.3 design proposes a supervised, local Python/MLX embedding worker alongside the existing Go application and Ollama chat engine. Retain it as a candidate for native video/joint encoding; 0.40.11 now provides a simpler image/audio embedding alternative to compare during qualification, not an already-qualified replacement. Index short video segments, retrieve them with text embeddings, and give Gemma a bounded set of retrieved images plus host-owned timestamps. Start with visual questions and visible citations. Audio-aware retrieval is feasible in the encoder; spoken-content answering needs another qualified serving path.

Google describes EmbeddingGemma 2 as a multimodal embedding model for text, images, audio, and video. Its intended use includes cross-modal retrieval; it does not generate answers. [Announcement](https://blog.google/innovation-and-ai/technology/developers-tools/embeddinggemma-2/)

| Component | Pin inspected | What is established | Remaining qualification |
| --- | --- | --- | --- |
| Native processor/model | `mlx-vlm==0.7.10`, Git commit `8c061ef516e7f8b584c0d5b64f26b50d5806303b` | Local `embedding_gemma2` implementation accepts text, image, video, and audio tensors | Install/import/forward on this Mac; resource use and retrieval accuracy |
| Converted weights | `mlx-community/embeddinggemma-2-bf16`, revision `1a4ffddb7905d3f63486748deabe091a01fb6201` | Public repository metadata and processor/config files inspected; 744,371,512 BF16 parameters | Download integrity, all tower weights, actual conversion parity |
| Historical Ollama embedding API | Previously installed release source `v0.32.3` | `/api/embed` accepts a string or a list of strings | Cannot supply native video/audio to this endpoint |
| Historical Ollama chat API | `v0.32.3` | Media support depends on backend/model; Gemma4 safetensors suppresses vision/audio, while GGUF projectors expose vision and can expose audio | Retrieve-image QA using a separately qualified GGUF model; app audio integration remains absent |
| Current app adapter | `internal/engine/adapter.go`, `engine.go` | Request images become inline image parts; non-image inline MIME types are rejected | New import/index/retrieval/UI path does not exist yet |

The MLX README/notebook currently names `nativ-community/embeddinggemma-2`; that repository was unavailable during this research. Use the verified `mlx-community` repository and immutable revision above. Do not silently follow a moving model name. Its BF16 tensor count implies about 1.49 GB of raw weights, **not** peak unified-memory use. [Pinned model repository](https://huggingface.co/mlx-community/embeddinggemma-2-bf16/tree/1a4ffddb7905d3f63486748deabe091a01fb6201), [MLX implementation](https://github.com/Blaizzy/mlx-vlm/tree/8c061ef516e7f8b584c0d5b64f26b50d5806303b/mlx_vlm/models/embedding_gemma2)

The historical 0.32.3 Ollama limitation comes from the handler's input-type switch, not merely a missing example; 0.40.11 changes that contract as documented below. A multimodal model card does not change the endpoint contract. [Embed handler](https://github.com/ollama/ollama/blob/v0.32.3/server/routes.go#L794), [request/message types](https://github.com/ollama/ollama/blob/v0.32.3/api/types.go)

## Exact native API surface

The following is an **integration sketch derived from source, not locally executed code**. After an explicitly approved download, load the immutable local snapshot rather than a remote repository identifier during ordinary runs:

```python
import mlx.core as mx
from mlx_vlm import load

model, processor = load(local_snapshot_directory)
query_inputs = processor.tokenizer(
    ["task: search result | query: " + question],
    padding=True,
    return_tensors="np",
)
query = model(**{k: mx.array(v) for k, v in query_inputs.items()}).text_embeds
mx.eval(query)
```

The model returns `EmbeddingOutput.text_embeds`, including for media inputs. Internally it scatters media features into text positions, applies mask-aware mean pooling, and L2-normalizes the result. Store the resulting finite float32 vector, not token hidden states. [Model forward](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/embedding_gemma2/embedding_gemma2.py#L77), [pooling](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/pooling.py)

For one already extracted chunk, the proposed direct processor call is:

```python
inputs = processor(
    text="<|video|><|audio|>",
    videos=[rgb_frames],            # One T,H,W,C uint8 RGB array
    audio=[mono_float32_16khz],      # Same source interval
    do_sample_frames=False,        # Host has already selected the frames
    input_data_format="channels_last",
    add_timestamps=False,
    max_soft_tokens=140,
    sampling_rate=16000,
    return_tensors="mlx",
)
metadata = inputs.pop("video_metadata", None)
if inputs["input_ids"].shape[-1] > 8192:
    raise ValueError("chunk exceeds the qualified embedding context")
chunk = model(**inputs).text_embeds
mx.eval(chunk)
```

For video-only chunks omit the audio placeholder and argument. The processor also supports `apply_chat_template(..., tokenize=True, return_dict=True, return_tensors="mlx")`, with typed media content. Its local processor converts NumPy preprocessing outputs to MLX arrays, without requiring PyTorch for this path. Direct and templated calls must be compared during qualification; the exact combined-array invocation above still needs execution. [Processor](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/embedding_gemma2/processing_embedding_gemma2.py), [upstream examples](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/examples/embedding_gemma2.ipynb)

Use the trained 8,192-token limit even though a config field permits larger positional indices. Keep full 768-dimensional vectors initially. BF16 and float32 are appropriate; FP16 is not. If later adopting 512/256-dimensional prefixes, truncate and renormalize both queries and corpus vectors. Text queries use the search prompt; pure media does not need a text task prefix. At the documented defaults, eight video frames use roughly 1,120 visual tokens and eight seconds of audio add roughly 200; always inspect actual expanded length. [Official model card](https://ai.google.dev/gemma/docs/embeddinggemma/model_card_2)

## Media handling and timestamps

Use eight-second windows with a six-second stride as the first frozen experiment, within the requested 6–12-second range. At one frame per second, each full window supplies eight frames. Retain a shorter final window. Persist each source interval and each selected frame's presentation timestamp independently of any model output. Overlap helps boundary events but also creates near-duplicate search hits; diversify returned intervals before selecting evidence.

The processor's default video policy is one FPS, at most 32 frames, uniform overflow, with timestamps disabled. `num_frames` is rejected; use `fps`/`max_frames`, or predecoded frames with sampling disabled. There is no public start/end clip-window argument in this processor: passing an entire movie does not implement chunking. [Video processor](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/embedding_gemma2/video_processing_embedding_gemma2.py)

**Do not use processor timestamps as an exact citation clock.** Its native decoder keeps packet PTS internally, but exports an average FPS and selected frame indices. Transformers computes `VideoMetadata.timestamps` as index divided by FPS; the prompt renderer then reduces these to MM:SS. Variable-frame-rate files can therefore have imprecise displayed times even when frame selection itself succeeds. Start the qualification corpus with constant-frame-rate media; either capture real source PTS in a host decoder or explicitly defer accurate VFR support. [Decoder metadata](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/embedding_gemma2/_video_decoder.py#L333), [Transformers timestamp calculation](https://github.com/huggingface/transformers/blob/v5.14.0/src/transformers/video_utils.py)

MLX-VLM's native decoder uses a checked FFmpeg 7 ABI from OpenCV and Metal color conversion. Unsupported layouts/builds fall back to OpenCV; orientation, HDR, interlacing, color range and VFR all require explicit tests. Upstream reports numerical decoder/model comparisons on its own M5 Max corpus. Those are useful upstream evidence, **not this app's test results or proof of retrieval quality**. [Decoder implementation](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/embedding_gemma2/_video_decoder.py), [upstream validation statement](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/embedding_gemma2/README.md)

Audio extraction needs a separate authority boundary. `load_audio_from_video=True` appends decoded audio to video content; it is false by default. For MP4 and several compressed audio formats, `mlx_audio.audio_io.read` invokes external `ffprobe` and `ffmpeg`, without a timeout in that helper. The loader also supports remote inputs. Do not let these implicit operations bypass the app's process or network policy. [Processor audio path](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/mlx_vlm/models/embedding_gemma2/processing_embedding_gemma2.py#L75), [audio I/O](https://github.com/Blaizzy/mlx-audio/blob/v0.5.2/mlx_audio/audio_io.py)

Prefer a host-owned bounded extraction job: selected local input, fixed argument construction, read-only source, private output directory, explicit timeout/cancellation, and disabled remote protocols. Feed decoded RGB frames and mono 16-kHz samples to the worker. This does not grant the coding agent an unrestricted shell or permission to spawn descendants. Record the extractor version, normalization policy, source interval, actual PTS and derivative hashes. Reject unsupported inputs honestly instead of silently discarding audio or changing timing.

## Where each responsibility belongs

| Owner | Responsibility |
| --- | --- |
| Go media service | Import consent, immutable local copy/hash, file/duration/pixel bounds, extraction lifecycle and provenance |
| Python MLX worker | Load approved local weights; encode validated arrays/text; return vectors and measured counts; no tools or agent instructions |
| Go persistence/retrieval | Chunk records, atomic index publication, model/preprocessing version compatibility, cosine ranking and temporal deduplication |
| Existing Gemma engine | Answer a bounded evidence request using retrieved images; no editing/terminal tools in this mode |
| UI | Import/index progress, cancellation/errors, evidence thumbnails, source-time links, explicit modality/coverage labels |

For the first local corpus, SQLite plus exact cosine dot products is sufficient; a new vector database is unnecessary. One 768-dimensional float32 vector is 3,072 bytes. Store asset ID/hash, chunk start/end milliseconds, frame PTS and hashes, vector modality, dimension, model revision, preprocessing fingerprint and indexing status. Index a video-only vector first; add a separate joint video/audio vector experimentally so an audio ablation remains possible. Do not overwrite one modality with another under an identical cache key.

Publish only a complete, compatible index. Interrupted jobs may retain completed chunks for deterministic restart, but incomplete assets must not quietly appear fully searchable. A changed model, dimension, decoding policy or input hash invalidates reuse. Preserve the original asset and evidence receipt independently of embeddings.

Use a single shared inference admission policy across Ollama and MLX initially. The existing Ollama semaphore cannot govern a new process by itself. Serialize embedding and generation, bound per-job input, and measure actual unified-memory peak before enabling concurrency. Worker cancellation may need process termination because Python cannot promise immediate interruption inside a GPU operation; use atomic chunk commits and reclaim the worker cleanly. Offline startup must fail explicitly if approved weights are missing instead of initiating a download.

## Answering and the audio boundary

Retrieve three temporally distinct chunks and send a bounded chronological frame selection, initially six to nine images total, with immutable chunk IDs and timestamps. The host maps cited IDs back to real intervals and rejects invented IDs. Similarity is a ranking score, not a confidence probability. The answer prompt should require evidence-based statements and acknowledge when sampled frames cannot establish an event. Clip text, subtitles and speech are untrusted evidence, not instructions granting new authority.

The existing Gemma adapter can transport images, making visual QA the shortest integration path. Under the previously installed Ollama 0.32.3, `gemma4:12b-mlx` could not supply vision and the historical fallback below was GGUF. Under the now-installed 0.40.11, both existing Gemma MLX models advertise vision; qualify these before deciding whether any additional answer model is needed. Text-only coding success does not establish image understanding. Keep the original clip available for playback beside the answer. A frame-derived answer must not imply continuous viewing of the whole video.

Joint embeddings may retrieve a segment for an audible event without providing words that the current image-only app adapter can hear. Therefore initially label such results as retrieved clips and permit playback, but decline unsupported spoken quotations. To answer speech questions, separately qualify timestamped transcription or an audio-capable adapter. **Correction from deeper runtime inspection:** lacking a separate `Message.Audio` field does not mean Ollama is wholly audio-incapable. Version 0.32.3 detects WAV/MP3 media and has `/v1/audio/transcriptions`; its tests expose audio for Gemma4 GGUF projectors while suppressing audio for Gemma4 safetensors. Our adapter currently rejects non-image inline MIME, and this report has not tested any audio generation/transcription path. [Media detection](https://github.com/ollama/ollama/blob/v0.32.3/llm/media.go), [transcription middleware](https://github.com/ollama/ollama/blob/v0.32.3/middleware/openai.go#L724), [capability tests](https://github.com/ollama/ollama/blob/v0.32.3/server/images_test.go#L477)

## Python 3.13 / 3.14 dependency setup

Use a dedicated project-owned ARM64 environment. Do not modify shared system Python, global packages or another project's inference environment. Python 3.13 is a reasonable first qualification target; 3.14 is also metadata-compatible and should not be described as unsupported without a resolver/import test.

| Proposed pin | Inspected compatibility evidence |
| --- | --- |
| `mlx-vlm==0.7.10` | Requires Python >=3.10; upstream classifiers stop at 3.12 and inspected CI uses 3.10, so neither 3.13 nor 3.14 is proven by that CI |
| `mlx==0.32.4` and matching Metal package | Meets MLX-VLM minimum; PyPI has ARM64 CPython 3.13 and 3.14 wheels |
| `transformers==5.14.0` | Universal wheel; native MLX processor is implemented locally, avoiding a requirement to install upstream development Transformers |
| `mlx-audio==0.5.2` | Minimum requested by MLX-VLM; base dependency path does not require Torch |
| `opencv-python==5.0.0.93` | ARM64 ABI3 wheel; upstream video validation names this FFmpeg 7 bundle |
| `tokenizers==0.22.2` | Fits Transformers' inspected version interval; ARM64 ABI3 wheel. Do not pin unpublished `0.23.0` merely because it appears as an upper bound |
| Remaining native dependencies | Metadata/wheels checked for miniaudio 1.71, NumPy 2.5.4, SciPy 1.18.1, SentencePiece 0.2.2 and llguidance 1.9.1; both interpreters have eligible wheels |

These are candidate constraints, **not a resolved lock file**. MLX-VLM also pulls packages such as datasets and server dependencies. The full transitive graph, installation size, macOS minimum, interpreter architecture, imports and `pip check` remain untested. Resolve the environment once authorized, freeze exact versions/hashes, record installed wheel tags, and perform offline reload before calling it supported. Avoid optional UI/training/CUDA extras and unnecessary Torch audio/video backends. [MLX-VLM metadata](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/pyproject.toml), [requirements](https://github.com/Blaizzy/mlx-vlm/blob/v0.7.10/requirements.txt), [MLX wheels](https://pypi.org/project/mlx/0.32.4/#files), [Transformers metadata](https://pypi.org/pypi/transformers/5.14.0/json), [OpenCV wheels](https://pypi.org/project/opencv-python/5.0.0.93/#files), [tokenizers wheels](https://pypi.org/project/tokenizers/0.22.2/#files)

## Qualification before enabling the feature

1. **Environment and encoder contract.** Resolve/import on the chosen interpreter; no Torch import or remote execution; load the pinned local snapshot offline. Encode text, image, video-only, audio-only and joint input. Assert shape `(1,768)`, finite values and unit norms; compare batching against individual calls and save/reload. Verify expanded tokens and actual media counts, not estimates.
2. **Frozen visual retrieval challenge.** Author a 60–90-second clip with six distinct visible events and distractors. Keep the ground-truth sidecar and held-out questions outside model inputs. Require at least five of six queries to return a ground-truth-overlapping interval in top three, and save all ranks/scores. This is a small deterministic acceptance task, not a general benchmark.
3. **Answer grounding.** Score visual answers separately from retrieval. Every citation must resolve to supplied evidence. Include negative and ambiguous questions; require an explicit inability to establish unsupported details. Check that playback seeks to the correct source interval. Save prompt, selected frame hashes, answer and independent rubric outcome.
4. **Audio ablation.** Keep frames identical and replace the audio track with distinct known events. Compare video-only and joint/audio retrieval. Do not mark speech-answer support passed merely because the audio changes rankings. A transcription or native-audio path needs its own timestamp and word-accuracy test.
5. **Media robustness.** Check silent video, short final chunks, corrupt/truncated inputs, repeated frames, rotation and color range. Qualify CFR first; add VFR and exact PTS assertions before claiming accurate VFR citations. Exercise the codec/decoder combinations the product actually permits.
6. **Lifecycle and authority.** Cancel extraction and active embedding, kill/restart the worker, restart the app during indexing, re-import identical data, and change preprocessing/model pins. Require no published partial index, no duplicate chunks and no unexpected downloads/network. Test path traversal, external symlinks, media URLs and prompt injection embedded in content.
7. **Measured costs.** Record host/model/runtime identity, wall time, peak unified memory, per-stage timings, disk growth and cancellation latency. Keep first-load and warm-run measurements separate; repeat before reporting performance. Only then decide whether concurrent Ollama/MLX operation fits this Mac.

The first deliverable should be a narrow, repeatable visual clip-QA capability with timestamped evidence. Accurate audio answers, long-library retrieval, broader codec coverage and concurrent model residency should advance only when their separate evidence exists.

## Historical 0.32.3 answer-model fallback: GGUF

The earlier inspection on 2026-10-27 pinned the then-installed Ollama `v0.32.3` to commit `fce745fe5e4f0c7741ca46fc5484c9471c997d00`. This version explicitly removes vision and audio capabilities whenever the Gemma4 renderer uses the safetensors format. Tests cover the small, large and legacy renderer variants. The MLX completion client forwards prompt/options but no media, and the inspected Gemma4 forward operates on token embeddings. The family webpage and registry configuration can therefore advertise vision that this runtime intentionally withholds. `gemma4:e4b-mlx` will not repair the installed `gemma4:12b-mlx` limitation. [Capability filter](https://github.com/ollama/ollama/blob/v0.32.3/server/images.go#L452), [MLX completion transport](https://github.com/ollama/ollama/blob/v0.32.3/x/mlxrunner/client.go#L141), [Gemma4 forward](https://github.com/ollama/ollama/blob/v0.32.3/x/models/gemma4/gemma4.go#L987)

**Historical fallback candidate under 0.32.3: `gemma4:e4b-it-q4_K_M`, the GGUF variant. This is not a current download recommendation.** Its remote manifest includes the language model, a separate BF16 image projector and a draft model. It requires Ollama >=0.30.9, below our pinned version. This is source/manifest-supported vision, still requiring local installation and image acceptance tests before product enablement.

| Exact tag | Manifest digest inspected | Total layer bytes | Decision |
| --- | --- | ---: | --- |
| `gemma4:e4b-it-q4_K_M` | `sha256:dc35e8d9c6061baa6f0fa870975ab6932e2542b579b13ea0f199fa4bb7300c9c` | 6,583,656,264 | First GGUF vision candidate; 991,552,256-byte projector included |
| `gemma4:e2b-it-q4_K_M` | `sha256:b37049369adfe3d2b653af0ab301a062ca5cbe96ace6aa0d3f2559ee9b563fc2` | 4,587,156,264 | Smaller alternative if storage or measured memory limits rule out E4B; quality must be evaluated separately |
| `gemma4:e4b-mlx` | `sha256:5a08735df03c348ab10d56e8ad758098ff1960ab6030605c95fde7d90fc9de18` | 9,461,575,918 | Safetensors; vision/audio suppressed by pinned runtime despite registry capability flags |

These sizes come from small manifest/config JSON reads; **no model tensor blobs were downloaded**. Byte counts describe stored layers, not resident memory. The registry request for generic `e4b` returned the same GGUF digest in this inspection, while the website offers backend-specific alternatives. Prefer the exact explicit-format tag, and verify the resolved digest after an authorized pull rather than relying on the moving alias. [E4B GGUF manifest](https://registry.ollama.ai/v2/library/gemma4/manifests/e4b-it-q4_K_M), [E2B GGUF manifest](https://registry.ollama.ai/v2/library/gemma4/manifests/e2b-it-q4_K_M), [MLX manifest](https://registry.ollama.ai/v2/library/gemma4/manifests/e4b-mlx), [E4B backend listing](https://ollama.com/library/gemma4:e4b)

If the historical GGUF fallback is later selected and authorized, require local `/api/show` to report `vision` and its expected GGUF/projector metadata. Then run a real single-image contrast test, a multi-image order test with different source timestamps, and an unseen-frame negative control through **our Engine**, not only Ollama CLI. Record model digest, runtime version, decoded image hashes, prompts and raw responses. Use a bounded context such as 16K initially and begin with three images before increasing to the proposed six to nine; the advertised 128K context does not establish practical memory or image throughput. Unload the coding model and serialize MLX embeddings with GGUF generation until measured memory supports another policy. Do not silently switch to a remote/cloud tag if a local model fails.


## 0.40.11 source addendum — 2026-10-27

This addendum pins **Ollama v0.40.11, commit `675150d6ad2747df1e218ef59de9194bc4a7e538`**. It updates runtime assumptions only. The Python processor sketches, model pins, historic manifest reads, and absence of local media qualification above remain unchanged. No dependency installation, model pull or inference was performed for this update.

**Embedding contract.** Native `/api/embed` now accepts a string, a list of strings, one object with `text`, `image`, and/or `audio`, or a mixed batch of strings and such objects. Object fields must be strings; `image` and `audio` contain base64-encoded bytes. Each batch item owns its media and receives its own output vector. It is not an array-of-frames contract: `input.video` is an explicit error, an image list is not accepted as an object field, and batching frames does not imply temporal fusion into one clip vector. The ordinary Gemma chat models are not substitutes for a dedicated embedding encoder. [Pinned request type](https://github.com/ollama/ollama/blob/v0.40.11/api/types.go#L646), [exact input validation](https://github.com/ollama/ollama/blob/v0.40.11/server/routes.go#L1115).

The release contains the `EmbeddingGemma2Model` MLX implementation under `gemma4embedding`, with shared Gemma4 image/audio towers, projection, mean pooling and normalized output. Media support is checked against loaded tower presence; unsupported inputs fail explicitly. Output dimensions are validated against declared trained sizes and truncated outputs are renormalized. The runner accounts for expanded media tokens and can reject context/capacity overflow; server-side text truncation is not a guarantee that expanded media fits. Keep `truncate: false` during qualification to expose over-limit inputs and record returned token counts. This establishes a source-supported image/audio candidate, not conversion parity, retrieval quality, a downloaded embedding model, or native video support. [Introducing commit](https://github.com/ollama/ollama/commit/efe43c5561047d8baf8c862730b2151249d51fd5), [encoder](https://github.com/ollama/ollama/blob/v0.40.11/mlxrunner/model/gemma4embedding/gemma4embedding.go), [media embedding path](https://github.com/ollama/ollama/blob/v0.40.11/mlxrunner/embed.go#L196), [output normalization](https://github.com/ollama/ollama/blob/v0.40.11/server/routes.go#L1341).

For example, the source accepts the following **schema sketch; it has not been executed**:

```json
{
  "model": "<qualified-local-embedding-model>",
  "input": [
    "task: search result | query: a person opens a red door",
    {"image": "<base64-image-bytes>"},
    {"audio": "<base64-audio-bytes>"}
  ],
  "truncate": false
}
```

These are three independent vectors, not one jointly encoded video. A frame-wise index with host-owned intervals and separately stored audio vectors is a possible alternative experiment. It changes the representation from the original eight-frame joint/video vector, so compare retrieval behavior and preserve distinct preprocessing/index identities. Keep the Python video encoder candidate until that comparison exists; do not present frame batching as equivalent to native video encoding.

**Answering with installed MLX models.** The upstream change implements image processing for the Gemma4 26B/31B/e-series vision tower and the 12B unified embedder, plus audio for checkpoints that carry an audio path. It removes the previous Gemma4 safetensors capability suppression; existing imports can expose their recorded capabilities without reimporting. The 26B/31B have no audio configuration and reject audio. [Media-support commit](https://github.com/ollama/ollama/commit/b79067b0db7417f20108363bc22adb97f35c966a), [pinned media implementation](https://github.com/ollama/ollama/blob/v0.40.11/mlxrunner/model/gemma4/media.go#L139), [current capability filter](https://github.com/ollama/ollama/blob/v0.40.11/server/images.go#L502).

The saved local metadata agrees with that distinction: `gemma4:26b-mlx` advertises completion/vision/tools/thinking; `gemma4:12b-mlx` additionally advertises audio. Both are safetensors with `gemma4-large` rendering and the `gemma4` parser. These are capability declarations, not successful image/audio tests. The current application adapter still rejects non-image inline MIME types, so 12B runtime audio support does not enable audio answering in this application. [Local metadata](../evidence/ollama040-model-metadata.json), [application attachment boundary](../../internal/engine/adapter.go).

**Revised prerequisite.** Remove “download a GGUF answer model” from the required path. Once the core coding gate is satisfied and media qualification is authorized, use the already-installed model candidates for the single-image contrast, multi-image order and unseen-frame controls through the Engine. Only choose a separate GGUF fallback if those tests or measured memory/latency justify it. Speech answering remains gated on an audio-capable app path and its own independent tests. No previous test result is upgraded by this source inspection, and video-RAG implementation remains deferred.

## Read-only prerequisite check — 2026-10-27, after the 0.40.11 update

**The extension condition is still unmet.** The integration owner observed that a fresh live compaction lost exact command constraints and is repairing the core first. This section records preparation only; it authorizes no implementation or inference and does not change any earlier qualification result. No packages, model weights, or media were downloaded or installed during this check. Only executable versions, disk usage, installed-model metadata, public registry manifest metadata, and primary source documentation were read.

| Prerequisite | Observed fact | Qualification limit |
| --- | --- | --- |
| Local extraction tools | `/opt/homebrew/bin/ffmpeg` and `/opt/homebrew/bin/ffprobe` both report **8.1** | Version execution only; no clip was decoded and no extraction sandbox/cancellation test ran |
| Disk | `df -h . .runtime` reports approximately **137 GiB available** on their shared APFS Data volume; `.runtime` uses 545 MiB, `dist` 65 MiB, and `docs/evidence` 27 MiB | Point-in-time measurements, not a storage reservation or a media-job quota |
| Installed models | Read-only `/api/tags` lists `gemma4:12b-mlx` and `gemma4:26b-mlx` only | **No embedding model is installed**; installed chat models remain unqualified for image answering through this app |
| Official visual embedding candidate | `embeddinggemma-2:440m-mxfp8-text-vision`; manifest SHA-256 **`fc94c176e27b3109d869852b27871835b890dcc6742b1ef3d0fb2341f61e1263`** | Exact candidate for a future qualification, not a download instruction or quality claim |
| Candidate storage | Registry manifest contains **632 layers totaling 777,762,858 bytes**, plus a 212-byte config blob | Stored bytes, not peak memory; no tensor blobs were fetched |

The exact tag's configuration names `EmbeddingGemma2Model`, includes its vision tower, sets `audio_config: null`, and declares 768-dimensional embeddings. This is the text-plus-vision variant, not the similarly named first-generation text-only `embeddinggemma` model or the full 740M multimodal variant. Its official catalogue entry rounds the size to 778 MB. The manifest config digest is `sha256:fdf079ef341acc94f9db75ac0a1bd134d13c6385524977105793429f21b1dc81`; capture and verify the resolved model digest again after any future authorized pull. [Exact catalogue tag](https://ollama.com/library/embeddinggemma-2:440m-mxfp8-text-vision), [public manifest](https://registry.ollama.ai/v2/library/embeddinggemma-2/manifests/440m-mxfp8-text-vision), [model configuration](https://ollama.com/library/embeddinggemma-2:440m-mxfp8-text-vision/blobs/2fbe56872a17)

The smallest complete visual slice can therefore use **Ollama frame embeddings** without first installing the proposed Python worker. Its scope is local-clip import, independently embedded sampled frames, timestamp retrieval, image-grounded answers, and citations into local playback:

1. Import one bounded local CFR clip and preserve its immutable copy/hash. Start acceptance with the earlier 60–90-second visual fixture, rather than a whole library.
2. Run a host-owned extraction job with fixed arguments, disabled remote protocols, private bounded outputs, timeout/cancellation, and approximately one frame per second. Persist actual source timestamps and derivative hashes. Existing FFmpeg availability does not grant an agent unrestricted process authority.
3. Embed each image separately through `/api/embed`; store finite normalized vectors and source/model/preprocessing identities in SQLite. Publish only complete indexes. Add the embedding call to the existing shared inference admission path so indexing cannot compete invisibly with chat generation.
4. Embed the search query using `task: search result | query: ...`, rank frame vectors with exact cosine similarity, and select three temporally distinct evidence windows. Keep frame scores and timestamps; do not claim an unqualified average of frame vectors is a learned clip embedding.
5. Supply the existing Gemma engine with a bounded chronological image set and immutable evidence IDs. Show thumbnails and host-validated citations that seek the original clip. A scoped playback endpoint and import/index/query interface are implementation work still to do; do not expose arbitrary filesystem paths to the webview.

For the embedding request, 0.40.11 accepts an object such as `{"image":"<base64-image-bytes>"}`, text strings, or a batch of those items. Set `truncate: false`; initially retain the default 768 dimensions and use an 8,192-token policy with independently bounded image count, dimensions, bytes, and expanded media tokens. The catalogue displays a 256K context label derived from architectural metadata, while Google's model card specifies a trained **8,192-token** context; the larger label is not retrieval-quality evidence. Text queries need their task prefix; pure images do not. [Pinned API types](https://github.com/ollama/ollama/blob/v0.40.11/api/types.go#L646), [input validation and embedding handler](https://github.com/ollama/ollama/blob/v0.40.11/server/routes.go#L1115), [Google model card and query guidance](https://ai.google.dev/gemma/docs/embeddinggemma/model_card_2)

**This is frame-wise visual retrieval, not temporal video encoding.** `input.video` is explicitly rejected by the pinned Ollama handler, and an image batch produces one vector per item. Sampled-frame evidence cannot establish every action between frames, continuous motion, speech, or audio events. Retain the original Python/native-video encoder proposal as a later separately qualified representation; neither it nor a new GGUF answer-model download is a prerequisite for trying this narrower frame path.

No present disk/tool-installation blocker was identified. Before enabling the feature, the remaining gates are core qualification, authorized acquisition and offline reload of the pinned embedding candidate, single-image contrast/multi-image order/negative tests through our Engine, the frozen retrieval and answer-grounding challenge, timestamp/playback checks, interruption/restart behavior, and measured memory/disk/cancellation costs. These are unperformed checks, not inferred successes from model metadata.

## Implemented visual slice and measured outcome — 2026-10-27

The historical preparation sections above describe their observation time. The core coding condition was subsequently satisfied by a guided multi-turn repair passing 14 project tests, nine independent checks, six supplemental cases, protected inputs and no-tool recall. The authorized visual slice is now implemented; the Python/native joint-video representation remains a separate future option.

The exact 440M text/vision candidate was downloaded from Ollama's official registry and its resolved digest matched the pinned manifest. The production Go embedding client uses 768 normalized finite components, explicit image/text limits, `truncate:false`, pre/post model identity checks and shared inference admission. No Python worker was added. A six-image/six-query encoder probe returned the intended image as top-1 for all six queries. [Installation](../evidence/video-embedding-install.json), [encoder probe](../evidence/video-embedding-live-probe.json).

The implemented host path copies a scoped MP4 into private immutable storage, validates a bounded CFR/H.264 source, records actual decoded presentation timestamps, extracts up to 90 JPEG frames at approximately 1 Hz, and publishes a complete versioned SQLite index atomically. Text queries rank frames by exact cosine similarity and select up to three frames separated by at least two seconds. The no-tools answer call receives those chronological images and host-owned evidence labels. Citations resolve to frame identities and timestamps; generated paths and URLs cannot become media authority. HTTP Range playback uses expiring same-origin capability URLs.

The frozen 30-second synthetic challenge passed import, indexing, source-byte range verification, all six retrieval intervals and all seven answer/citation jobs in 36.348 seconds using Gemma 26B. Its six named scenes are simple static shapes, not a motion benchmark. Independent review recorded five unambiguous positive answers, a qualified yellow star/flower answer, and a negative response limited to the supplied frames. This does not establish order sensitivity, real-world video recall, or general hallucination resistance. [Final pipeline](../evidence/video-workflow-final.json), [semantic review](../evidence/video-final-semantic-review.json).

The original run remains a failed citation-format trial. Supporting grouped exact labels fixed that application defect; every member is still checked against retrieved evidence. Native desktop checks verified playback, 21-second citation seeking, persisted results after restart, real indexing cancellation at 6/30 frames without replacing the old ready index, and one correct 12B answer submitted through the native bridge. Automated fixtures cover interrupted-job reconciliation, cancellation cleanup, transactional admission, malformed/unknown citations, source changes, scope isolation, range handling and capability expiry. [Native record](../evidence/video-native-workflow.json), [regression summary](../evidence/video-release-backend-summary.json).

Current operational limits remain explicit: FFmpeg and model weights are external dependencies; audio is preserved but not analyzed; sampled frames can miss intervening actions; rotated/VFR/HDR/nonzero-start/multiple-video-stream sources are rejected. An abrupt process crash can leave unpublished private files, though no partial index becomes searchable and no job silently replays. Application cancellation does not measure the end of an already-running GPU kernel. The saved memory/disk observation is point-in-time, not peak usage or a broad capacity qualification. These limits and the synthetic fixture's ambiguity remain part of the release claim.
