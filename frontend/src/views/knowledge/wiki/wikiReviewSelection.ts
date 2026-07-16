export interface WikiReviewSelectableSet {
  id: string
  change_category?: string
}

export interface WikiReviewBatchSelectionState {
  checked: boolean
  indeterminate: boolean
  selected: number
  total: number
}

export function selectableWikiReviewIds(sets: WikiReviewSelectableSet[]): string[] {
  return sets.filter(set => set.change_category !== 'conflict').map(set => set.id)
}

export function normalizeWikiReviewSelection(selectedIds: string[], sets: WikiReviewSelectableSet[]): string[] {
  const selectable = new Set(selectableWikiReviewIds(sets))
  return Array.from(new Set(selectedIds)).filter(id => selectable.has(id))
}

export function wikiReviewBatchSelectionState(
  selectedIds: string[],
  sets: WikiReviewSelectableSet[],
): WikiReviewBatchSelectionState {
  const total = selectableWikiReviewIds(sets).length
  const selected = normalizeWikiReviewSelection(selectedIds, sets).length
  return {
    checked: total > 0 && selected === total,
    indeterminate: selected > 0 && selected < total,
    selected,
    total,
  }
}

export function toggleAllWikiReviews(
  selectedIds: string[],
  sets: WikiReviewSelectableSet[],
  checked: boolean,
): string[] {
  if (!checked) return []
  return selectableWikiReviewIds(sets)
}

export function toggleWikiReview(
  selectedIds: string[],
  sets: WikiReviewSelectableSet[],
  id: string,
  checked: boolean,
): string[] {
  const normalized = normalizeWikiReviewSelection(selectedIds, sets)
  if (!selectableWikiReviewIds(sets).includes(id)) return normalized
  if (!checked) return normalized.filter(selectedId => selectedId !== id)
  return Array.from(new Set([...normalized, id]))
}
