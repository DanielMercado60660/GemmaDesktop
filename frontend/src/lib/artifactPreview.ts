import type { Artifact } from "../types";

export type ArtifactPreviewState =
  | { status: "idle"; id: "" }
  | { status: "loading"; id: string }
  | { status: "ready"; id: string; artifact: Artifact }
  | { status: "error"; id: string; error: string };

// Retain only the selected artifact's contents. Polling metadata must never
// overwrite a fetched snapshot, and late selection responses cannot replace it.
export function createArtifactLoader(
  fetchArtifact: (id: string) => Promise<Artifact>,
  publish: (state: ArtifactPreviewState) => void,
) {
  let generation = 0;
  return {
    invalidate() {
      generation++;
    },
    async select(id: string) {
      const request = ++generation;
      if (!id) {
        publish({ status: "idle", id: "" });
        return;
      }
      publish({ status: "loading", id });
      try {
        const artifact = await fetchArtifact(id);
        if (request !== generation) return;
        if (!artifact || artifact.id !== id)
          throw new Error(
            "The server returned a different artifact. Select it again to retry.",
          );
        publish({ status: "ready", id, artifact });
      } catch (error) {
        if (request === generation)
          publish({
            status: "error",
            id,
            error:
              error instanceof Error
                ? error.message
                : "Artifact contents could not be loaded.",
          });
      }
    },
  };
}

export function artifactContent(artifact?: Artifact) {
  if (typeof artifact?.after !== "string") return { kind: "none" } as const;
  if (typeof artifact.before === "string")
    return {
      kind: "diff",
      before: artifact.before,
      after: artifact.after,
    } as const;
  return { kind: "text", after: artifact.after } as const;
}
