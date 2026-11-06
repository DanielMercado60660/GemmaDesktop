import { emptyState, type State } from "../../src/types";

export function fixture(): State {
  const state = structuredClone(emptyState);
  state.runtime = { available: true, version: "test" };
  state.workspaces = [
    { id: "w", name: "Fixture project", path: "/fixture", createdAt: "" },
  ];
  state.sessions = [
    {
      id: "s",
      workspaceId: "w",
      title: "Fixture chat",
      createdAt: "",
      updatedAt: "",
    },
  ];
  state.agents = [
    {
      id: "default",
      name: "Gemma",
      instructions: "",
      model: "",
      maxSteps: 24,
      mode: "approval",
    },
  ];
  state.models = [
    {
      name: state.settings.defaultModel,
      capabilities: ["completion"],
      size: 1,
      digest: "test",
      loaded: false,
      family: "gemma",
      parameterSize: "12B",
      quantization: "Q4",
    },
  ];
  state.workflows = [
    {
      id: "review",
      revision: "v1",
      name: "Review",
      description: "",
      nodes: [
        {
          id: "agent",
          type: "agent",
          label: "Reviewer",
          position: { x: 0, y: 0 },
          prompt: "Review this task",
        },
      ],
      edges: [],
    },
  ];
  return state;
}

export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

export function managedFixture(): State {
  const state = fixture();
  state.managedRuntime = {
    catalog: [
      {
        id: "mlx-v1",
        provider: "omlx",
        sourceSha: "abcdef0123456789",
        architecture: "arm64",
        minimumOs: "14.0",
        license: "Apache-2.0",
        readyForInference: true,
        bundle: {
          size: 100000000,
          url: "https://example.invalid/runtime.tar.gz",
          sha256: "verified",
        },
        files: [{ path: "runtime", size: 200000000, sha256: "verified" }],
        qualification: { transport: "qualified", coding: "experimental" },
      },
    ],
    modelCatalog: [
      {
        id: "gemma-native",
        repository: "example/gemma",
        revision: "abcdef0123456789",
        serveName: "native-gemma",
        license: "Gemma",
        providers: ["omlx"],
        model: {
          name: "native-gemma",
          contextTokens: 65536,
          artifactDigest: "verified",
          capabilities: {
            completion: true,
            tools: true,
            thinking: true,
            vision: false,
            structuredOutput: true,
          },
        },
        files: [
          {
            path: "model.safetensors",
            size: 2000000000,
            sha256: "verified",
            source: {
              size: 2000000000,
              url: "https://example.invalid/model.safetensors",
            },
          },
        ],
      },
    ],
    installed: [],
    models: [],
    selections: {},
    jobs: [],
    process: { buildId: "", modelId: "", status: "stopped" },
    platformSupport: { "mlx-v1": "supported" },
  };
  return state;
}
