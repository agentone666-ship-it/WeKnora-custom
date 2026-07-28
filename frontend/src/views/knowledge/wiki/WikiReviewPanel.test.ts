import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const component = readFileSync(new URL('./WikiReviewPanel.vue', import.meta.url), 'utf8')

test('review content is rendered as sanitized markdown', () => {
  assert.match(component, /sanitizeMarkdownHTML\(marked\.parse/)
  assert.match(component, /v-html="renderMarkdown\(selectedFeedback\.feedback_text\)"/)
  assert.match(component, /class="markdown-content final-preview"/)
})

test('wiki links, citations, and relationship codes are rendered as business-friendly labels', () => {
  assert.match(component, /function prepareWikiMarkdown/)
  assert.match(component, /wiki-review-link/)
  assert.match(component, /wiki-review-citation/)
  assert.match(component, /consistent: '内容一致'/)
  assert.match(component, /conflicting: '说法冲突'/)
  assert.match(component, /marked\.parse\(prepareWikiMarkdown\(source\)/)
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

test('merge duplicate review loads the existing target page before comparison', () => {
  assert.match(component, /possibleDuplicateTargetSlug\(item\)/)
  assert.match(component, /getWikiPage\(props\.knowledgeBaseId, targetSlug\)/)
  assert.match(component, /withPossibleDuplicateBefore\(item, pageResponse\?\.data \|\| pageResponse\)/)
})

test('merge duplicate approval always submits the referenced existing page as merge target', () => {
  assert.match(component, /const resolvedMergeIntoSlug = decision === 'approved' && isMergeDuplicate\.value/)
  assert.match(component, /mergeTargetSlug\.value\.trim\(\) \|\| possibleDuplicateTargetSlug/)
  assert.match(component, /const isMergeApproval = decision === 'approved' && Boolean\(resolvedMergeIntoSlug\)/)
  assert.match(component, /if \(isMergeApproval\)/)
  assert.match(component, /decisionChoices\.value\[id\] !== 'existing'/)
  assert.match(component, /patch\.content = value\.content\n\s+patch\.summary = value\.summary/)
  assert.match(component, /merge_into_slug: resolvedMergeIntoSlug \|\| undefined/)
  assert.match(component, /isMergeDuplicate \? '合并到现有知识并发布'/)
})

test('manual merging an addition preserves existing content when that choice is selected', () => {
  assert.match(component, /const isMergeApproval = decision === 'approved' && Boolean\(resolvedMergeIntoSlug\)/)
  assert.match(component, /if \(isMergeApproval\) \{\n\s+if \(decisionChoices\.value\[id\] && decisionChoices\.value\[id\] !== 'existing'\)/)
})

test('manual merging an addition explicitly submits feedback or custom content', () => {
  assert.match(component, /decisionChoices\.value\[id\] !== 'existing'/)
  assert.match(component, /patch\.content = value\.content\n\s+patch\.summary = value\.summary/)
})

test('only the latest detail request may update the active review', () => {
  assert.match(component, /const requestVersion = \+\+detailRequestVersion/)
  assert.match(component, /if \(requestVersion !== detailRequestVersion\) return\n    selected\.value = detail/)
  assert.match(component, /if \(requestVersion === detailRequestVersion\) detailLoading\.value = false/)
})
