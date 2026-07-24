import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./WikiBrowser.vue', import.meta.url), 'utf8')

test('wiki card knowledge type is visible in reader and graph drawer metadata', () => {
  assert.match(source, /selectedPage\.page_type === 'card' && selectedPage\.knowledge_type/)
  assert.match(source, /getKnowledgeTypeLabel\(selectedPage\.knowledge_type\)/)
  assert.match(source, /graphDrawerPage\.page_type === 'card' && graphDrawerPage\.knowledge_type/)
  assert.match(source, /getKnowledgeTypeLabel\(graphDrawerPage\.knowledge_type\)/)
})

test('all supported wiki knowledge types have localized label keys', () => {
  for (const type of [
    'knowledge',
    'experience',
    'question',
    'hypothesis',
    'experiment',
    'metric',
    'procedure',
    'failure',
    'case',
    'rule',
  ]) {
    assert.match(source, new RegExp(`\\b${type}: '${type}'`))
  }
})
