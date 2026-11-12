import type { AspectRatio } from '../../api/client'

export const ASPECT_RATIOS: { value: AspectRatio; label: string }[] = [
  { value: '3:4', label: '3:4 portrait (catalogue)' },
  { value: '4:5', label: '4:5 portrait (social)' },
  { value: '2:3', label: '2:3 portrait' },
  { value: '1:1', label: '1:1 square' },
  { value: '9:16', label: '9:16 tall (stories)' },
  { value: '4:3', label: '4:3 landscape' },
  { value: '16:9', label: '16:9 wide (banner)' },
]
