export function filterSessions(sessions, filter, nameFn) {
  if (!filter) return sessions;
  const q = filter.toLowerCase();
  return sessions.filter(s => nameFn(s).toLowerCase().includes(q));
}
