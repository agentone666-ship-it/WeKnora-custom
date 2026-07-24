import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const component = readFileSync(new URL('./WikiReviewPanel.vue', import.meta.url), 'utf8')

test('review content is rendered as sanitized markdown', () => {
  assert.match(component, /sanitizeMarkdownHTML\(marked\.parse/)
  assert.match(component, /v-html="renderMarkdown\(selectedFeedback\.feedback_text\)"/)
  assert.match(component, /class="markdown-content final-preview"/)
})

test('the content editor only appears after the reviewer chooses custom editing', () => {
  assert.match(component, /v-if="decisionChoices\[item\.id\] === 'custom' && overrides\[item\.id\]"/)
  assert.match(component, /v-else class="choice-hint"/)
  assert.doesNotMatch(component, /@focus="markCustom/)
})

test('technical identifiers stay inside collapsed diagnostic details', () => {
  assert.match(component, /<details class="diagnostic-details">/)
  assert.match(component, /管理员排查编号/)
  assert.doesNotMatch(component, />待处理信号</)
  assert.doesNotMatch(component, />反馈驱动的知识更新</)
})
