export function limitNote(file: number, runtime: number): string | null {
  if (file === runtime) return null
  return `本次实际 ${runtime}`
}
