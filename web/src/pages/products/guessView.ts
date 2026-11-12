import type { View } from '../../api/client'

/** Guess the view from the file name; the user can change it before uploading. */
export function guessView(name: string): View {
  const n = name.toLowerCase()
  if (/back|rear/.test(n)) return 'back'
  if (/side|profile/.test(n)) return 'side'
  if (/print|logo|embroider/.test(n)) return 'print'
  if (/detail|close|macro/.test(n)) return 'detail'
  return 'front'
}
