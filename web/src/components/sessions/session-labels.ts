import type { SessionEntry } from '../../types/api'

/**
 * Display title for a session. Titles are taken from the first prompt, so they
 * can run to several lines; lists and headings show them collapsed to one.
 */
export function getSessionLabel(session: Pick<SessionEntry, 'title'>) {
  return session.title?.replace(/\s+/g, ' ').trim() || 'Untitled session'
}

export function getSessionProjectLabel(session: Pick<SessionEntry, 'project_name' | 'project_id'>) {
  return session.project_name || session.project_id || 'No linked project'
}
