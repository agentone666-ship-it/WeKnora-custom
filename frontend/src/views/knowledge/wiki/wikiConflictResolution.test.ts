import assert from 'node:assert/strict'
import test from 'node:test'
import { allConflictsSelected, buildConflictChoices, conflictChoiceKey, conflictReviewDecision } from './wikiConflictResolution.ts'

test('builds one independent choice for every conflict position', () => {
  const selections = {
    [conflictChoiceKey('item-1', 0)]: 'adopt_candidate' as const,
    [conflictChoiceKey('item-1', 1)]: 'keep_existing' as const,
  }
  assert.deepEqual(buildConflictChoices('item-1', 2, selections), [
    { item_id: 'item-1', assessment_index: 0, resolution: 'adopt_candidate' },
    { item_id: 'item-1', assessment_index: 1, resolution: 'keep_existing' },
  ])
  assert.equal(allConflictsSelected('item-1', 2, selections), true)
})

test('does not complete review while any conflict is unselected', () => {
  const selections = { [conflictChoiceKey('item-1', 0)]: 'keep_existing' as const }
  assert.equal(allConflictsSelected('item-1', 2, selections), false)
})

test('approves mixed choices and rejects when every candidate claim is discarded', () => {
  assert.equal(conflictReviewDecision([
    { item_id: 'item-1', assessment_index: 0, resolution: 'keep_existing' },
    { item_id: 'item-1', assessment_index: 1, resolution: 'adopt_candidate' },
  ]), 'approved')
  assert.equal(conflictReviewDecision([
    { item_id: 'item-1', assessment_index: 0, resolution: 'keep_existing' },
  ]), 'rejected')
})
