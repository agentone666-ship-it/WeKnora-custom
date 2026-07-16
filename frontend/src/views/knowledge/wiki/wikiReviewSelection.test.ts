import assert from 'node:assert/strict'
import test from 'node:test'

import {
  normalizeWikiReviewSelection,
  selectableWikiReviewIds,
  toggleAllWikiReviews,
  toggleWikiReview,
  wikiReviewBatchSelectionState,
  type WikiReviewSelectableSet,
} from './wikiReviewSelection.ts'

const sets: WikiReviewSelectableSet[] = [
  { id: 'addition-1', change_category: 'addition' },
  { id: 'conflict-1', change_category: 'conflict' },
  { id: 'correction-1', change_category: 'correction' },
]

test('select all includes every current batch-safe item and excludes conflicts', () => {
  assert.deepEqual(selectableWikiReviewIds(sets), ['addition-1', 'correction-1'])
  assert.deepEqual(toggleAllWikiReviews([], sets, true), ['addition-1', 'correction-1'])
})

test('select all toggles back to an empty selection', () => {
  assert.deepEqual(toggleAllWikiReviews(['addition-1', 'correction-1'], sets, false), [])
})

test('selection state supports unchecked, indeterminate, and checked states', () => {
  assert.deepEqual(wikiReviewBatchSelectionState([], sets), { checked: false, indeterminate: false, selected: 0, total: 2 })
  assert.deepEqual(wikiReviewBatchSelectionState(['addition-1'], sets), { checked: false, indeterminate: true, selected: 1, total: 2 })
  assert.deepEqual(wikiReviewBatchSelectionState(['addition-1', 'correction-1'], sets), { checked: true, indeterminate: false, selected: 2, total: 2 })
})

test('a conflict cannot be selected through the single-item toggle', () => {
  assert.deepEqual(toggleWikiReview([], sets, 'conflict-1', true), [])
})

test('refresh and filtering remove hidden, stale, and conflict IDs', () => {
  assert.deepEqual(
    normalizeWikiReviewSelection(['addition-1', 'hidden-1', 'conflict-1'], sets),
    ['addition-1'],
  )
})

test('deselecting one item after select all produces an indeterminate state', () => {
  const selected = toggleWikiReview(toggleAllWikiReviews([], sets, true), sets, 'addition-1', false)
  assert.deepEqual(selected, ['correction-1'])
  assert.equal(wikiReviewBatchSelectionState(selected, sets).indeterminate, true)
})
