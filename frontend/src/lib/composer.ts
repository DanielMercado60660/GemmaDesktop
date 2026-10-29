export interface ComposerOwner {
  workspaceId: string;
  sessionId: string;
}
export interface ComposerSelection extends ComposerOwner {
  generation: number;
}
export interface ComposerDraft {
  text: string;
  attachments: string[];
  revision: number;
}
export interface Submission extends ComposerSelection, ComposerDraft {
  id: number;
  agentId?: string;
  model?: string;
}

function ownerKey(owner: ComposerOwner) {
  return JSON.stringify([owner.workspaceId, owner.sessionId]);
}
const emptyDraft = (): ComposerDraft => ({
  text: "",
  attachments: [],
  revision: 0,
});

// Draft identity follows the conversation, never the currently focused view.
// Revisions also detect typing away and back to the same text during admission.
export function createComposerStore(
  initial: ComposerOwner,
  changed = () => {},
) {
  let selection: ComposerSelection = { ...initial, generation: 0 };
  const drafts = new Map<string, ComposerDraft>();
  let nextSubmission = 0;
  let pending: Submission | undefined;
  const read = (owner: ComposerOwner = selection) =>
    drafts.get(ownerKey(owner)) || emptyDraft();
  const isCurrent = (owner: ComposerSelection) =>
    ownerKey(selection) === ownerKey(owner) &&
    selection.generation === owner.generation;
  return {
    selection: () => selection,
    read,
    isCurrent,
    isSending: () => !!pending,
    select(owner: ComposerOwner) {
      if (ownerKey(owner) === ownerKey(selection)) return;
      selection = { ...owner, generation: selection.generation + 1 };
      changed();
    },
    update(patch: Partial<Pick<ComposerDraft, "text" | "attachments">>) {
      const previous = read();
      drafts.set(ownerKey(selection), {
        ...previous,
        ...patch,
        attachments: [...(patch.attachments || previous.attachments)],
        revision: previous.revision + 1,
      });
      changed();
    },
    remove(owner: ComposerOwner) {
      drafts.delete(ownerKey(owner));
      if (ownerKey(selection) === ownerKey(owner))
        selection = {
          workspaceId: owner.workspaceId,
          sessionId: "",
          generation: selection.generation + 1,
        };
      changed();
    },
    begin(
      options: { agentId?: string; model?: string } = {},
    ): Submission | undefined {
      const draft = read();
      if (pending || !selection.workspaceId || !draft.text.trim()) return;
      pending = {
        ...selection,
        ...draft,
        attachments: [...draft.attachments],
        ...options,
        id: ++nextSubmission,
      };
      changed();
      return pending;
    },
    accept(submission: Submission, sessionId: string) {
      if (pending?.id !== submission.id) return;
      const key = ownerKey(submission);
      const current = read(submission);
      if (current.revision === submission.revision)
        drafts.set(key, { ...emptyDraft(), revision: current.revision + 1 });
      // Only the still-selected new-chat composer adopts the newly created
      // session. A late receipt must never navigate back from another chat.
      if (!submission.sessionId && isCurrent(submission)) {
        const target = { workspaceId: submission.workspaceId, sessionId };
        drafts.set(ownerKey(target), read(submission));
        drafts.delete(key);
        selection = { ...target, generation: selection.generation + 1 };
      }
      changed();
    },
    finish(submission: Submission) {
      if (pending?.id !== submission.id) return;
      pending = undefined;
      changed();
    },
  };
}

export type ComposerStore = ReturnType<typeof createComposerStore>;
export interface ComposerAPI {
  createSession(input: {
    workspaceId: string;
    title: string;
  }): Promise<{ id: string }>;
  startRun(input: {
    sessionId: string;
    prompt: string;
    attachments: string[];
    agentId?: string;
    model?: string;
  }): Promise<unknown>;
}

// Navigation can proceed while the explicitly submitted work is admitted, but
// every request stays bound to the captured owner and draft. Failures keep it.
export async function submitDraft(
  store: ComposerStore,
  submission: Submission,
  api: ComposerAPI,
) {
  let sessionId = submission.sessionId;
  if (!sessionId) {
    const created = await api.createSession({
      workspaceId: submission.workspaceId,
      title: submission.text.slice(0, 70),
    });
    sessionId = created.id;
    if (!sessionId)
      throw new Error("The new conversation did not receive an ID.");
  }
  await api.startRun({
    sessionId,
    prompt: submission.text.trim(),
    attachments: [...submission.attachments],
    agentId: submission.agentId,
    model: submission.model,
  });
  store.accept(submission, sessionId);
}
