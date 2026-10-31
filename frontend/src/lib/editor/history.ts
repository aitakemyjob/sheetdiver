// Undo and redo: snapshots of what's being edited, newest last.

/** A bounded undo history of snapshots. */
export class History<T> {
  private past: T[] = [];
  private future: T[] = [];

  constructor(private readonly limit = 200) {}

  /** Remembers the state before a change; a change drops what was undone. */
  push(before: T): void {
    this.past.push(before);
    if (this.past.length > this.limit) this.past.shift();
    this.future = [];
  }

  /** The state to go back to from now, or null. */
  undo(now: T): T | null {
    const prev = this.past.pop();
    if (prev === undefined) return null;
    this.future.push(now);
    return prev;
  }

  /** The state to go forward to from now, or null. */
  redo(now: T): T | null {
    const next = this.future.pop();
    if (next === undefined) return null;
    this.past.push(now);
    return next;
  }

  get canUndo(): boolean {
    return this.past.length > 0;
  }

  get canRedo(): boolean {
    return this.future.length > 0;
  }
}
