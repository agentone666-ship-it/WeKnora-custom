import assert from 'node:assert/strict'
import test from 'node:test'

import {
  crossPageConflictExistingContent,
  possibleDuplicateTargetSlug,
  usesCrossPageConflictFallback,
  wikiComparisonBeforeContent,
  withPossibleDuplicateBefore,
} from './wikiConflictComparison.ts'

test('uses the stored before page for a same-page update', () => {
  const item = {
    change_category: 'conflict',
    before: { content: '# 原知识\n\n原来的完整内容' },
    after: { page_metadata: { cross_page_assessments: [] } },
  }

  assert.equal(wikiComparisonBeforeContent(item), '# 原知识\n\n原来的完整内容')
  assert.equal(usesCrossPageConflictFallback(item), false)
})

test('falls back to related page evidence for a cross-page conflict create', () => {
  const item = {
    change_category: 'conflict',
    after: {
      page_metadata: {
        cross_page_assessments: [{
          related_slug: 'card/rule-current-fee',
          relation: 'conflicting',
          existing_claim: '旧的收费说法',
        }],
        related_page_evidence: {
          'card/rule-current-fee': {
            title: '当前收费规则',
            excerpt: '# 当前收费规则\n\n自由市场功能主要向卖家收费。',
          },
        },
      },
    },
  }

  assert.equal(
    crossPageConflictExistingContent(item),
    '## 当前收费规则\n\n**发生冲突的说法**\n\n旧的收费说法\n\n### 原知识页面内容\n\n# 当前收费规则\n\n自由市场功能主要向卖家收费。',
  )
  assert.equal(
    wikiComparisonBeforeContent(item),
    '## 当前收费规则\n\n**发生冲突的说法**\n\n旧的收费说法\n\n### 原知识页面内容\n\n# 当前收费规则\n\n自由市场功能主要向卖家收费。',
  )
  assert.equal(usesCrossPageConflictFallback(item), true)
})

test('uses the existing claim when an old historical snapshot has no evidence excerpt', () => {
  const item = {
    change_category: 'conflict',
    after: {
      page_metadata: {
        cross_page_assessments: [{
          related_slug: 'card/rule-current-fee',
          relation: 'supersedes',
          existing_claim: '知识库当前采用旧规则。',
        }],
      },
    },
  }

  assert.equal(wikiComparisonBeforeContent(item), '## rule current fee\n\n**发生冲突的说法**\n\n知识库当前采用旧规则。')
})

test('combines multiple conflicting knowledge pages with readable headings', () => {
  const item = {
    change_category: 'conflict',
    after: {
      page_metadata: {
        cross_page_assessments: [
          { related_slug: 'card/rule-a', relation: 'conflicting', existing_claim: '说法 A' },
          { related_slug: 'card/rule-b', relation: 'supersedes', existing_claim: '说法 B' },
          { related_slug: 'card/rule-c', relation: 'consistent', existing_claim: '不应展示' },
        ],
        related_page_evidence: {
          'card/rule-a': { title: '规则甲', excerpt: '规则甲正文' },
          'card/rule-b': { title: '规则乙', excerpt: '规则乙正文' },
        },
      },
    },
  }

  assert.equal(
    wikiComparisonBeforeContent(item),
    '## 规则甲\n\n**发生冲突的说法**\n\n说法 A\n\n### 原知识页面内容\n\n规则甲正文\n\n## 规则乙\n\n**发生冲突的说法**\n\n说法 B\n\n### 原知识页面内容\n\n规则乙正文',
  )
})

test('groups multiple conflict claims from the same existing knowledge page', () => {
  const item = {
    change_category: 'conflict',
    after: {
      page_metadata: {
        cross_page_assessments: [
          { related_slug: 'card/rule-a', relation: 'conflicting', existing_claim: '手机报价时间为 24 小时。' },
          { related_slug: 'card/rule-a', relation: 'conflicting', existing_claim: '其他品类报价时间为 48 小时。' },
        ],
        related_page_evidence: {
          'card/rule-a': { title: '报价时效规则', excerpt: '这是原知识页的上下文，只应展示一次。' },
        },
      },
    },
  }

  assert.equal(
    wikiComparisonBeforeContent(item),
    '## 报价时效规则\n\n**发生冲突的说法**\n\n- 手机报价时间为 24 小时。\n- 其他品类报价时间为 48 小时。\n\n### 原知识页面内容\n\n这是原知识页的上下文，只应展示一次。',
  )
})

test('does not borrow related evidence for a non-conflict change', () => {
  const item = {
    change_category: 'addition',
    after: {
      page_metadata: {
        cross_page_assessments: [{ related_slug: 'card/rule-a', relation: 'conflicting', existing_claim: '旧说法' }],
      },
    },
  }

  assert.equal(wikiComparisonBeforeContent(item), '')
  assert.equal(usesCrossPageConflictFallback(item), false)
})

test('hydrates a merge duplicate with the referenced existing page', () => {
  const item = {
    change_category: 'merge_duplicate',
    before: {},
    after: {
      content: '# 新说法',
      page_metadata: {
        possible_duplicate_slug: 'card/knowledge-existing',
      },
    },
  }
  const target = {
    slug: 'card/knowledge-existing',
    title: '已有知识',
    content: '# 已有知识\n\n这是知识库当前采用的说法。',
  }

  assert.equal(possibleDuplicateTargetSlug(item), 'card/knowledge-existing')
  const hydrated = withPossibleDuplicateBefore(item, target)
  assert.equal(wikiComparisonBeforeContent(hydrated), target.content)
  assert.notEqual(hydrated, item)
})

test('does not overwrite a stored before snapshot when hydrating a duplicate', () => {
  const item = {
    change_category: 'merge_duplicate',
    before: { content: '审核创建时保存的旧内容' },
    after: { page_metadata: { possible_duplicate_slug: 'card/knowledge-existing' } },
  }

  assert.equal(withPossibleDuplicateBefore(item, { content: '后来版本' }), item)
})
