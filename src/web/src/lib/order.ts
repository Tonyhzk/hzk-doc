export function moveItemBy<T>(
  items: readonly T[],
  index: number,
  delta: number,
): T[] {
  const copy = items.slice()
  const target = index + delta
  if (
    index < 0 ||
    index >= copy.length ||
    target < 0 ||
    target >= copy.length ||
    delta === 0
  ) {
    return copy
  }
  const [item] = copy.splice(index, 1)
  copy.splice(target, 0, item as T)
  return copy
}

export function canMove(index: number, length: number, delta: number): boolean {
  const target = index + delta
  return (
    delta !== 0 &&
    index >= 0 &&
    index < length &&
    target >= 0 &&
    target < length
  )
}

export function orderNames(entries: readonly { name: string }[]): string[] {
  return entries.map((entry) => entry.name)
}
