// Load draft state for settings tab

export interface SectionState<T, C> {
  loaded: T;
  draft: T;
  context: C;
  saving: boolean;
  error: string;
}

export type SectionAction<T, C> =
  | { type: "load"; value: T; context: C; error?: string }
  | { type: "rebase"; value: T; context: C; error: string }
  | { type: "edit"; patch: Partial<T> }
  | { type: "commit"; apply: (value: T) => T }
  | { type: "discard" }
  | { type: "saving" }
  | { type: "failed"; error: string };

export function initSection<T, C>(value: T, context: C): SectionState<T, C> {
  return { loaded: value, draft: value, context, saving: false, error: "" };
}

export function sectionReducer<T, C>(
  state: SectionState<T, C>,
  action: SectionAction<T, C>,
): SectionState<T, C> {
  switch (action.type) {
    case "load":
      return {
        loaded: action.value,
        draft: action.value,
        context: action.context,
        saving: false,
        error: action.error ?? "",
      };
    // Partial save reloads server state
    case "rebase":
      return {
        ...state,
        loaded: action.value,
        context: action.context,
        saving: false,
        error: action.error,
      };
    case "edit":
      return { ...state, draft: { ...state.draft, ...action.patch }, error: "" };
    // Save updates copies, discard keeps it
    case "commit":
      return { ...state, loaded: action.apply(state.loaded), draft: action.apply(state.draft) };
    case "discard":
      return { ...state, draft: state.loaded, error: "" };
    case "saving":
      return { ...state, saving: true, error: "" };
    case "failed":
      return { ...state, saving: false, error: action.error };
  }
}
