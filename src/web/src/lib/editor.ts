export interface EditorSnapshot {
  content: string
  savedContent: string
  revision: string
  conflict: boolean
}

export function createEditorSnapshot(
  content: string,
  revision: string,
): EditorSnapshot {
  return {
    content,
    savedContent: content,
    revision,
    conflict: false,
  }
}

export function isEditorDirty(snapshot: EditorSnapshot): boolean {
  return snapshot.content !== snapshot.savedContent
}

export function applySaveSuccess(
  snapshot: EditorSnapshot,
  revision: string,
  savedContent = snapshot.content,
): EditorSnapshot {
  return {
    ...snapshot,
    savedContent,
    revision,
    conflict: false,
  }
}

export function applySaveConflict(
  snapshot: EditorSnapshot,
): EditorSnapshot {
  return {
    ...snapshot,
    conflict: true,
  }
}

export function applyReload(
  snapshot: EditorSnapshot,
  content: string,
  revision: string,
): EditorSnapshot {
  return {
    content,
    savedContent: content,
    revision,
    conflict: false,
  }
}
