// Determines which action handleInput should take based on input state.
// Returns: 'shell' | 'slash' | 'managed' | 'instruct'
export function resolveInputAction(shellMode, sessionMode, inputText) {
  if (sessionMode === 'managed') {
    if (shellMode) return 'shell';
    if (inputText.trim().startsWith('/')) return 'slash';
    return 'managed';
  }
  return 'instruct';
}

if (typeof window !== 'undefined') {
  window._ccResolveInputAction = resolveInputAction;
}
