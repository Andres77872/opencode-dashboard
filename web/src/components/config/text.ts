/**
 * Present a server-supplied message as a sentence. Diagnostics from the API
 * arrive lowercase and unpunctuated ("source discovery is paused while
 * disabled"); the dashboard writes in sentence case.
 */
export function asSentence(text: string | null | undefined): string {
  const trimmed = (text ?? '').trim()
  if (!trimmed) return ''
  const capitalized = trimmed.charAt(0).toUpperCase() + trimmed.slice(1)
  return /[.!?…]$/.test(capitalized) ? capitalized : `${capitalized}.`
}
