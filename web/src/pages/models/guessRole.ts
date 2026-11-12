import type { ModelImageRole } from '../../api/client'

/** Guess the role from the file name; the user can change it before uploading. */
export function guessRole(name: string): ModelImageRole {
  return /full|body|outfit|standing/i.test(name) ? 'full_body' : 'identity'
}
