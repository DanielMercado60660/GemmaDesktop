import type { Model } from "../types";

export const VIDEO_EMBEDDING_MODEL = "embeddinggemma-2:440m-mxfp8-text-vision";

export function isCompletionModel(
  model: Pick<Model, "name" | "remote" | "capabilities">,
): boolean {
  if (model.remote) return false;
  const capabilities = model.capabilities || [];
  if (capabilities.length) return capabilities.includes("completion");
  // A newly downloaded encoder may appear before /api/show has populated its
  // capabilities. Do not briefly offer that known encoder as a chat model.
  return !model.name
    .split("/")
    .at(-1)
    ?.toLowerCase()
    .startsWith("embeddinggemma");
}

export function isVisionAnswerModel(model: Model): boolean {
  return isCompletionModel(model) && !!model.capabilities?.includes("vision");
}
