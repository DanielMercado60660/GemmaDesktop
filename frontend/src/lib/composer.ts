import type { VerificationContract } from "../types";

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
  focusedRunId?: string;
  revision: number;
}
export interface Submission extends ComposerSelection, ComposerDraft {
  id: number;
  agentId?: string;
  model?: string;
  draftRevision?: number;
  verificationContract?: VerificationContract;
}

export interface StoredDraft extends ComposerOwner {
  text: string;
  attachments: string[];
  focusedRunId?: string;
  revision: number;
}
export interface DraftPersistence {
  get(owner: ComposerOwner): Promise<StoredDraft>;
  save(
    input: ComposerOwner & {
      text: string;
      attachments: string[];
      focusedRunId?: string;
      expectedRevision: number;
    },
  ): Promise<StoredDraft>;
}
interface DraftSync {
  loaded: boolean;
  revision: number;
  savedLocalRevision: number;
  error: string;
  conflict?: boolean;
  loading?: Promise<void>;
  saving?: Promise<void>;
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
  persistence?: DraftPersistence,
) {
  let selection: ComposerSelection = { ...initial, generation: 0 };
  const drafts = new Map<string, ComposerDraft>();
  let nextSubmission = 0;
  let pending: Submission | undefined;
  const syncs = new Map<string, DraftSync>();
  const timers = new Map<string, ReturnType<typeof setTimeout>>();
  const read = (owner: ComposerOwner = selection) =>
    drafts.get(ownerKey(owner)) || emptyDraft();
  const isCurrent = (owner: ComposerSelection) =>
    ownerKey(selection) === ownerKey(owner) &&
    selection.generation === owner.generation;
  function sync(owner: ComposerOwner) {
    const key = ownerKey(owner);
    let state = syncs.get(key);
    if (!state) {
      state = {
        loaded: !persistence || !owner.workspaceId,
        revision: 0,
        savedLocalRevision: 0,
        error: "",
      };
      syncs.set(key, state);
    }
    return state;
  }
  async function load(owner: ComposerOwner = selection): Promise<void> {
    if (!persistence || !owner.workspaceId) return;
    const state = sync(owner),
      key = ownerKey(owner);
    if (state.loaded) return;
    if (state.loading) return state.loading;
    const before = read(owner).revision,
      hadLocalDraft = drafts.has(key);
    state.loading = (async () => {
      try {
        const stored = await persistence.get(owner);
        if (ownerKey(stored) !== key)
          throw new Error(
            "The service returned a draft for a different conversation.",
          );
        if (syncs.get(key) !== state) return;
        state.revision = stored.revision;
        if (!hadLocalDraft && read(owner).revision === before) {
          drafts.set(key, {
            text: stored.text,
            attachments: [...stored.attachments],
            ...(stored.focusedRunId
              ? { focusedRunId: stored.focusedRunId }
              : {}),
            revision: before + 1,
          });
          state.savedLocalRevision = before + 1;
        } else state.savedLocalRevision = -1;
        state.loaded = true;
        state.error = "";
      } catch (error) {
        state.error = (error as Error).message;
        throw error;
      } finally {
        state.loading = undefined;
        changed();
      }
    })();
    changed();
    return state.loading;
  }
  async function saveSnapshot(owner: ComposerOwner, snapshot: ComposerDraft) {
    if (!persistence || !owner.workspaceId) return;
    await load(owner);
    const state = sync(owner);
    // Every owner has one writer; an older response cannot overwrite the
    // revision used by a newer save, and conflicts never become blind writes.
    while (state.saving) await state.saving;
    if (state.conflict) throw new Error(state.error);
    if (state.savedLocalRevision === snapshot.revision) return;
    state.saving = (async () => {
      try {
        const stored = await persistence.save({
          ...owner,
          text: snapshot.text,
          attachments: [...snapshot.attachments],
          ...(snapshot.focusedRunId
            ? { focusedRunId: snapshot.focusedRunId }
            : {}),
          expectedRevision: state.revision,
        });
        if (ownerKey(stored) !== ownerKey(owner))
          throw new Error(
            "The service saved a different conversation's draft.",
          );
        state.revision = stored.revision;
        state.savedLocalRevision = snapshot.revision;
        state.error = "";
      } catch (error) {
        state.error = (error as Error).message;
        if (state.error.includes("revision conflict")) state.conflict = true;
        throw error;
      } finally {
        state.saving = undefined;
        changed();
      }
    })();
    changed();
    await state.saving;
  }
  async function flush(owner: ComposerOwner = selection) {
    await load(owner);
    if (pending && ownerKey(pending) === ownerKey(owner)) return;
    do {
      const snapshot = read(owner);
      await saveSnapshot(owner, snapshot);
      if (snapshot.revision === read(owner).revision) return;
    } while (!pending || ownerKey(pending) !== ownerKey(owner));
  }
  function schedule(owner: ComposerOwner) {
    if (!persistence || !owner.workspaceId) return;
    const key = ownerKey(owner);
    clearTimeout(timers.get(key));
    timers.set(
      key,
      setTimeout(() => {
        timers.delete(key);
        void flush(owner).catch(() => {});
      }, 250),
    );
  }
  return {
    selection: () => selection,
    read,
    isCurrent,
    isSending: () => !!pending,
    hasUnsaved: () =>
      !!persistence &&
      (timers.size > 0 ||
        Array.from(syncs.values()).some((state) => !!state.saving) ||
        Array.from(drafts.entries()).some(
          ([key, draft]) =>
            draft.revision !== syncs.get(key)?.savedLocalRevision,
        )),
    load,
    flush,
    async restore(owner: ComposerOwner = selection) {
      if (!persistence) return;
      const state = sync(owner),
        key = ownerKey(owner),
        before = read(owner).revision;
      clearTimeout(timers.get(key));
      timers.delete(key);
      while (state.saving) {
        try {
          await state.saving;
        } catch {
          /* Restore can recover a failed save. */
        }
      }
      const stored = await persistence.get(owner);
      if (ownerKey(stored) !== key)
        throw new Error(
          "The service returned a draft for a different conversation.",
        );
      if (syncs.get(key) !== state || read(owner).revision !== before)
        throw new Error(
          "The draft changed while restoring. Your current text was kept.",
        );
      drafts.set(key, {
        text: stored.text,
        attachments: [...stored.attachments],
        ...(stored.focusedRunId ? { focusedRunId: stored.focusedRunId } : {}),
        revision: before + 1,
      });
      Object.assign(state, {
        loaded: true,
        revision: stored.revision,
        savedLocalRevision: before + 1,
        error: "",
        conflict: false,
      });
      changed();
    },
    persistenceStatus(owner: ComposerOwner = selection) {
      const state = sync(owner);
      return {
        loaded: state.loaded,
        saving: !!state.saving || !!timers.get(ownerKey(owner)),
        error: state.error,
        conflict: !!state.conflict,
      };
    },
    select(owner: ComposerOwner) {
      if (ownerKey(owner) === ownerKey(selection)) return;
      selection = { ...owner, generation: selection.generation + 1 };
      changed();
    },
    update(
      patch: Partial<
        Pick<ComposerDraft, "text" | "attachments" | "focusedRunId">
      >,
    ) {
      const previous = read();
      drafts.set(ownerKey(selection), {
        ...previous,
        ...patch,
        attachments: [...(patch.attachments || previous.attachments)],
        revision: previous.revision + 1,
      });
      schedule({ ...selection });
      changed();
    },
    remove(owner: ComposerOwner) {
      clearTimeout(timers.get(ownerKey(owner)));
      timers.delete(ownerKey(owner));
      syncs.delete(ownerKey(owner));
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
      options: {
        agentId?: string;
        model?: string;
        verificationContract?: VerificationContract;
      } = {},
    ): Submission | undefined {
      const draft = read();
      if (pending || !selection.workspaceId || !draft.text.trim()) return;
      const { verificationContract: contract, ...rest } = options;
      pending = {
        ...selection,
        ...draft,
        attachments: [...draft.attachments],
        ...rest,
        // A focused continuation is bound to its source run's frozen contract.
        ...(contract && !draft.focusedRunId
          ? {
              verificationContract: {
                ...contract,
                command: [...contract.command],
                protectedPaths: [...contract.protectedPaths],
              },
            }
          : {}),
        id: ++nextSubmission,
      };
      changed();
      return pending;
    },
    async prepareSubmission(submission: Submission) {
      if (!persistence) return;
      clearTimeout(timers.get(ownerKey(submission)));
      timers.delete(ownerKey(submission));
      await saveSnapshot(submission, submission);
      submission.draftRevision = sync(submission).revision;
    },
    accept(
      submission: Submission,
      sessionId: string,
      receipt?: { clearedDraftRevision?: number; draftRevision?: number },
    ) {
      if (pending?.id !== submission.id) return;
      const key = ownerKey(submission);
      const current = read(submission);
      const cleared =
        !persistence || receipt?.clearedDraftRevision !== undefined;
      if (cleared && current.revision === submission.revision)
        drafts.set(key, { ...emptyDraft(), revision: current.revision + 1 });
      if (persistence) {
        const state = sync(submission);
        if (receipt?.clearedDraftRevision !== undefined) {
          state.revision = receipt.clearedDraftRevision;
          state.savedLocalRevision =
            current.revision === submission.revision
              ? read(submission).revision
              : -1;
        } else {
          // Another client may have saved a newer draft. Keep that server
          // revision untouched; the local text remains available to copy.
          state.conflict = true;
          if (receipt?.draftRevision !== undefined)
            state.revision = receipt.draftRevision;
          state.error =
            "The task started, but the saved draft changed or could not be cleared. Copy any text you need, then restore the saved draft before sending again.";
        }
      }
      // Only the still-selected new-chat composer adopts the newly created
      // session. A late receipt must never navigate back from another chat.
      if (cleared && !submission.sessionId && isCurrent(submission)) {
        const target = { workspaceId: submission.workspaceId, sessionId };
        drafts.set(ownerKey(target), read(submission));
        drafts.delete(key);
        // The source was cleared by admission. Newer local typing follows the
        // admitted chat and is saved under that new owner after loading it.
        const sourceSync = sync(submission);
        sourceSync.savedLocalRevision = 0;
        selection = { ...target, generation: selection.generation + 1 };
        schedule(target);
      }
      changed();
    },
    finish(submission: Submission) {
      if (pending?.id !== submission.id) return;
      pending = undefined;
      schedule({
        workspaceId: submission.workspaceId,
        sessionId: submission.sessionId,
      });
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
    draftRevision?: number;
    draftSessionId?: string;
    verificationContract?: VerificationContract;
  }): Promise<unknown>;
}

// Navigation can proceed while the explicitly submitted work is admitted, but
// every request stays bound to the captured owner and draft. Failures keep it.
export async function submitDraft(
  store: ComposerStore,
  submission: Submission,
  api: ComposerAPI,
) {
  await store.prepareSubmission(submission);
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
  const receipt = await api.startRun({
    sessionId,
    prompt: submission.text.trim(),
    attachments: [...submission.attachments],
    agentId: submission.agentId,
    model: submission.model,
    ...(submission.verificationContract
      ? { verificationContract: submission.verificationContract }
      : {}),
    ...(submission.draftRevision === undefined
      ? {}
      : {
          draftRevision: submission.draftRevision,
          draftSessionId: submission.sessionId,
        }),
  });
  store.accept(
    submission,
    sessionId,
    receipt as
      | { clearedDraftRevision?: number; draftRevision?: number }
      | undefined,
  );
}
