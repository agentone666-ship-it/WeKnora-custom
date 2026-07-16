import type { WikiConflictChoice } from '@/api/wiki'

export type ConflictSelection = Record<string, WikiConflictChoice['resolution']>

export function conflictChoiceKey(itemId: string, assessmentIndex: number) {
  return `${itemId}:${assessmentIndex}`
}

export function buildConflictChoices(
  itemId: string,
  assessmentCount: number,
  selections: ConflictSelection,
): WikiConflictChoice[] {
  const choices: WikiConflictChoice[] = []
  for (let index = 0; index < assessmentCount; index += 1) {
    const resolution = selections[conflictChoiceKey(itemId, index)]
    if (resolution) choices.push({ item_id: itemId, assessment_index: index, resolution })
  }
  return choices
}

export function allConflictsSelected(itemId: string, assessmentCount: number, selections: ConflictSelection) {
  return buildConflictChoices(itemId, assessmentCount, selections).length === assessmentCount
}

export function conflictReviewDecision(choices: WikiConflictChoice[]): 'approved' | 'rejected' {
  return choices.some(choice => choice.resolution === 'adopt_candidate') ? 'approved' : 'rejected'
}
