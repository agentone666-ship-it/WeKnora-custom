<template>
  <t-drawer v-model:visible="visible" header="知识纠错审核" size="1180px" :footer="false" destroy-on-close>
    <div class="review-workbench">
      <header class="workflow-header">
        <div>
          <h3>知识纠错审核</h3>
          <p>判断哪个说法正确，必要时修改，然后发布。</p>
        </div>
        <div class="workflow-steps" aria-label="反馈处理流程">
          <div class="workflow-step done"><b>1</b><span>查看反馈</span></div>
          <i></i>
          <div class="workflow-step active"><b>2</b><span>选择说法</span></div>
          <i></i>
          <div class="workflow-step"><b>3</b><span>确认发布</span></div>
        </div>
      </header>
      <div class="review-layout">
      <aside class="review-list">
        <div class="review-filters">
          <t-select v-model="category" :options="categoryOptions" clearable placeholder="全部反馈类型" @change="load" />
          <t-button variant="outline" :loading="loading" @click="load">刷新</t-button>
        </div>
        <div class="queue-title"><span>待审核反馈</span><t-tag size="small" theme="primary">{{ sets.length }}</t-tag></div>
        <div v-if="batchSelection.total" class="batch-selector">
          <t-checkbox :checked="batchSelection.checked" :indeterminate="batchSelection.indeterminate"
            @change="toggleAllBatch">
            全选当前新增内容（{{ batchSelection.total }}）
          </t-checkbox>
        </div>
        <div v-if="selectedBatchIds.length" class="batch-actions">
          <span>已选 {{ selectedBatchIds.length }} 条新增内容</span>
          <t-button size="small" theme="primary" :loading="submitting" @click="submitBatch">批量确认并发布</t-button>
        </div>
        <div v-if="!loading && sets.length === 0" class="review-empty">暂无待处理反馈</div>
        <div v-for="set in sets" :key="set.id" class="review-row" :class="{ active: selected?.id === set.id }"
          @click="select(set)">
          <div class="review-row-top">
            <t-checkbox v-if="set.change_category === 'addition'" :checked="selectedBatchIds.includes(set.id)"
              aria-label="选择这条新增内容" @click.stop @change="toggleBatch(set.id, $event)" />
            <t-tag size="small" :theme="categoryTheme(set.change_category)">{{ categoryLabel(set.change_category) }}</t-tag>
            <span>{{ set.items?.[0]?.after?.title || set.items?.[0]?.page_slug || '未命名内容' }}</span>
          </div>
          <p class="queue-feedback">{{ feedbackFor(set)?.feedback_text || reasonText(set.reasons) }}</p>
          <div class="review-row-meta">
            <span>{{ sourceLabel(feedbackFor(set)?.source) }}</span>
            <span>{{ formatTime(set.created_at) }}</span>
          </div>
        </div>
      </aside>

      <main class="review-detail">
        <div v-if="detailLoading" class="review-loading"><t-loading /></div>
        <template v-else-if="selected">
          <div class="detail-head">
            <div>
              <span class="eyebrow">待审核内容</span>
              <h3>{{ selected.items?.[0]?.after?.title || selected.items?.[0]?.page_slug }}</h3>
              <div class="detail-meta"><t-tag :theme="categoryTheme(selected.change_category)">{{ categoryLabel(selected.change_category) }}</t-tag></div>
            </div>
            <div class="version-result"><span>审核通过后</span><strong>保存为新版本</strong><small>旧版本仍会保留，可随时恢复</small></div>
          </div>
          <div v-if="selected.items?.length > 1" class="affected-notice">
            <strong>这条反馈关联了 {{ selected.items.length }} 条知识</strong>
            <span>请逐条确认，发布时会一起更新。</span>
          </div>

          <section v-if="selectedFeedback" class="feedback-context">
            <div class="section-heading">
              <div><span class="section-index">01</span><div><strong>查看反馈</strong><p>用户认为当前说法哪里不准确</p></div></div>
            </div>
            <div class="markdown-content feedback-markdown" v-html="renderMarkdown(selectedFeedback.feedback_text)"></div>
            <div class="feedback-fields compact">
              <div v-if="selectedFeedback.original_question"><span>用户当时问</span><div class="markdown-content" v-html="renderMarkdown(selectedFeedback.original_question)"></div></div>
              <div v-if="selectedFeedback.answer_excerpt"><span>系统当时回答</span><div class="markdown-content" v-html="renderMarkdown(selectedFeedback.answer_excerpt)"></div></div>
              <div v-if="selectedFeedback.suggested_correction" class="suggestion"><span>根据反馈整理出的新说法</span><div class="markdown-content" v-html="renderMarkdown(selectedFeedback.suggested_correction)"></div></div>
            </div>
          </section>

          <section v-if="isConflict" class="conflict-box">
            <div class="section-heading"><div><span class="section-index">03</span><div><strong>发现说法不一致</strong><p>请逐项选择最终保留的说法</p></div></div></div>
            <div v-if="conflictAssessments.length === 0" class="conflict-empty">
              这是一条历史冲突审核，但快照中没有可逐项裁决的冲突位置。请在下方整单批准、拒绝或暂缓。
            </div>
            <div v-for="assessment in conflictAssessments" :key="`${assessment.itemId}:${assessment.assessmentIndex}`" class="conflict-assessment">
              <div class="conflict-head">
                <strong>{{ conflictTitle(assessment) }}</strong>
              </div>
              <div class="claim-grid">
                <button type="button" class="claim-option"
                  :class="{ selected: conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'adopt_candidate' }"
                  @click="chooseConflict(assessment.itemId, assessment.assessmentIndex, 'adopt_candidate')">
                  <span>反馈建议的新说法</span>
                  <strong v-if="conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'adopt_candidate'">✓ 保留这个说法</strong>
                  <div class="markdown-content claim-content" v-html="renderMarkdown(assessment.candidate_claim || '没有可展示的内容')"></div>
                </button>
                <button type="button" class="claim-option"
                  :class="{ selected: conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'keep_existing' }"
                  @click="chooseConflict(assessment.itemId, assessment.assessmentIndex, 'keep_existing')">
                  <span>知识库现在的说法</span>
                  <strong v-if="conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'keep_existing'">✓ 保留这个说法</strong>
                  <div class="markdown-content claim-content" v-html="renderMarkdown(assessment.existing_claim || '没有可展示的内容')"></div>
                </button>
              </div>
              <div class="conflict-reason">{{ conflictReason(assessment) }}</div>
              <details v-if="assessment.relatedEvidence[assessment.related_slug]" class="technical-details">
                <summary>查看支持现有说法的来源</summary>
                <div class="markdown-content evidence-content" v-html="renderMarkdown(evidenceText(assessment.relatedEvidence[assessment.related_slug]))"></div>
              </details>
            </div>
            <div v-if="conflictAssessments.length" class="conflict-progress" :class="{ complete: allConflictChoicesSelected }">
              已选择 {{ selectedConflictCount }}/{{ conflictAssessments.length }} 处；每一处都选择后才能提交
            </div>
          </section>
          <section v-for="item in selected.items" :key="item.id" class="change-item">
            <div class="section-heading">
              <div><span class="section-index">{{ isConflict ? '04' : '02' }}</span><div><strong>{{ isConflict ? '查看完整内容对比' : '哪个说法是对的？' }}</strong><p>{{ itemTitle(item) }} · {{ versionText(item) }}</p></div></div>
            </div>
            <div class="change-summary">
              <div class="change-summary-head">
                <div>
                  <strong>这次具体改了什么</strong>
                  <span>{{ diffSummary(item).description }}</span>
                </div>
                <div class="change-counts" aria-label="修改统计">
                  <span class="change-count removed">− {{ diffSummary(item).removedLines }} 行删除</span>
                  <span class="change-count added">＋ {{ diffSummary(item).addedLines }} 行新增</span>
                </div>
              </div>
              <template v-if="diffSummary(item).changed">
                <div class="diff-board diff-board--rendered">
                  <div class="diff-board-column diff-board-column--removed">
                    <div class="diff-board-title"><span class="diff-dot removed"></span><strong>{{ comparisonBeforeTitle(item) }}</strong><small>{{ comparisonBeforeSubtitle(item) }}</small></div>
                    <div class="markdown-content diff-rendered-content" v-html="renderMarkdown(comparisonBefore(item))"></div>
                  </div>
                  <div class="diff-board-column diff-board-column--added">
                    <div class="diff-board-title"><span class="diff-dot added"></span><strong>{{ comparisonAfterTitle(item) }}</strong><small>{{ comparisonAfterSubtitle(item) }}</small></div>
                    <div class="markdown-content diff-rendered-content" v-html="renderMarkdown(comparisonAfter(item))"></div>
                  </div>
                </div>
                <details class="diff-source-details">
                  <summary>查看逐行差异</summary>
                  <div class="diff-board diff-board--source">
                    <div class="diff-board-column">
                      <div class="diff-board-title"><span class="diff-dot removed"></span><strong>删除内容</strong><small>Markdown 源文</small></div>
                      <div class="diff-lines">
                        <div v-for="(row, index) in diffRows(item).before" :key="`before-${item.id}-${index}`" class="diff-line" :class="`diff-line--${row.type}`">
                          <span class="diff-line-number">{{ row.number || '' }}</span><span class="diff-line-marker">{{ row.type === 'removed' ? '−' : ' ' }}</span><code>{{ row.text || ' ' }}</code>
                        </div>
                        <div v-if="!diffRows(item).before.length" class="diff-empty">（原内容为空）</div>
                      </div>
                    </div>
                    <div class="diff-board-column">
                      <div class="diff-board-title"><span class="diff-dot added"></span><strong>新增内容</strong><small>Markdown 源文</small></div>
                      <div class="diff-lines">
                        <div v-for="(row, index) in diffRows(item).after" :key="`after-${item.id}-${index}`" class="diff-line" :class="`diff-line--${row.type}`">
                          <span class="diff-line-number">{{ row.number || '' }}</span><span class="diff-line-marker">{{ row.type === 'added' ? '＋' : ' ' }}</span><code>{{ row.text || ' ' }}</code>
                        </div>
                        <div v-if="!diffRows(item).after.length" class="diff-empty">（新内容为空）</div>
                      </div>
                    </div>
                  </div>
                </details>
              </template>
              <div v-else class="diff-unchanged">未检测到正文变化，主要是元数据或关联关系调整。</div>
            </div>
            <template v-if="!isConflict">
            <div class="decision-grid">
              <button type="button" class="decision-option" :class="{ selected: decisionChoices[item.id] === 'existing' }" @click="chooseDecision(item, 'existing')">
                <span class="decision-label">保留现有说法</span><strong v-if="decisionChoices[item.id] === 'existing'">✓ 已选择</strong>
                <div class="markdown-content decision-preview" v-html="renderMarkdown(comparisonBefore(item))"></div>
              </button>
              <button type="button" class="decision-option" :class="{ selected: decisionChoices[item.id] === 'feedback' }" :disabled="!hasAutoContentChange(item)" @click="chooseDecision(item, 'feedback')">
                <span class="decision-label">采用反馈说法</span><strong v-if="decisionChoices[item.id] === 'feedback'">✓ 已选择</strong>
                <div v-if="hasAutoContentChange(item)" class="markdown-content decision-preview" v-html="renderMarkdown(selectedFeedback?.suggested_correction || pageContent(item.after) || '（暂无内容）')"></div>
                <p v-else class="decision-unavailable">未能在该 Wiki 页中定位原回答，系统没有自动拼接反馈。请使用“我来修改”并直接修正页面内容。</p>
              </button>
              <button type="button" class="decision-option" :class="{ selected: decisionChoices[item.id] === 'custom' }" @click="chooseDecision(item, 'custom')">
                <span class="decision-label">两者都不准确，我来修改</span><strong v-if="decisionChoices[item.id] === 'custom'">✓ 已选择</strong>
                <p>从当前内容开始编辑，写出最终应发布的说法。</p>
              </button>
            </div>
            <div v-if="decisionChoices[item.id]" class="final-editor">
              <div class="diff-title">
                <span>最终采用的说法</span>
                <t-button v-if="decisionChoices[item.id] !== 'custom'" size="small" variant="text" @click="chooseDecision(item, 'custom')">需要微调</t-button>
              </div>
              <t-textarea v-if="decisionChoices[item.id] === 'custom' && overrides[item.id]" v-model="overrides[item.id].content" class="candidate-editor" :autosize="{ minRows: 10, maxRows: 22 }" placeholder="写下最终要发布的内容，可使用标题、列表等格式" />
              <div v-else class="markdown-content final-preview" v-html="renderMarkdown(overrides[item.id]?.content || '')"></div>
            </div>
            <div v-else class="choice-hint">请先从上面选择一个说法。选择前不会出现编辑框。</div>
            </template>
            <details class="technical-details">
              <summary>查看来源与排查信息</summary>
              <div class="technical-summary">
                <span>知识条目：{{ itemTitle(item) }}</span>
                <span>反馈来源：{{ sourceLabel(selectedFeedback?.source) }}</span>
                <span>建议关注：{{ attentionLabel(selectedFeedback?.risk_level) }}</span>
              </div>
              <div v-for="(sourceExcerpt, sourceId) in item.evidence_excerpts || {}" :key="sourceId" class="evidence-excerpt"><div class="diff-title">引用来源</div><div class="markdown-content evidence-content" v-html="renderMarkdown(String(sourceExcerpt))"></div></div>
              <details class="diagnostic-details"><summary>管理员排查编号</summary><pre>{{ diagnosticText(item) }}</pre></details>
            </details>
          </section>

          <details v-if="selected.items?.[0]?.operation === 'create' && !isConflict" class="merge-box technical-details">
            <summary>这条内容与现有知识重复？</summary>
            <div class="merge-controls"><t-input v-model="mergeTargetSlug" placeholder="填写要合并到的知识路径" /><t-button variant="outline" :disabled="!mergeTargetSlug.trim()" :loading="submitting" @click="submit('approved', mergeTargetSlug)">合并并发布</t-button></div>
          </details>
          <section class="publish-card">
            <div class="section-heading"><div><span class="section-index">{{ isConflict ? '05' : '03' }}</span><div><strong>确认发布</strong><p>发布后会生成一个新版本，之后仍可回滚</p></div></div></div>
            <details class="review-note"><summary>添加审核说明（可选）</summary><t-textarea v-model="comment" :autosize="{ minRows: 2, maxRows: 5 }" placeholder="拒绝反馈时请填写原因" /></details>
          <div class="review-actions">
            <template v-if="isConflict">
              <t-button variant="outline" :loading="submitting" @click="deferConflict">暂缓审核</t-button>
              <template v-if="conflictAssessments.length">
                <t-button theme="primary" :disabled="!allConflictChoicesSelected" :loading="submitting" @click="submitConflict">提交逐项裁决</t-button>
              </template>
              <template v-else>
                <t-button theme="danger" variant="outline" :loading="submitting" @click="submitWholeConflict('rejected')">整单拒绝</t-button>
                <t-button theme="primary" :loading="submitting" @click="submitWholeConflict('approved')">整单批准并发布</t-button>
              </template>
            </template>
            <template v-else>
              <t-button theme="danger" variant="outline" :loading="submitting" @click="submit('rejected')">拒绝这条反馈</t-button>
              <t-button theme="primary" :disabled="!allDecisionsMade" :loading="submitting" @click="submit('approved')">确认修改并发布</t-button>
            </template>
          </div>
          </section>
        </template>
        <div v-else class="review-empty">从左侧选择一条反馈，开始审核</div>
      </main>
      </div>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { marked } from 'marked'
import { batchReviewWikiChangeSets, getWikiChangeSet, listWikiChangeSets, listWikiFeedbackSignals, reviewWikiChangeSet, type WikiChangeCategory, type WikiChangeItem, type WikiChangeSet, type WikiConflictChoice, type WikiFeedbackSignal } from '@/api/wiki'
import { sanitizeMarkdownHTML } from '@/utils/security'
import { allConflictPositionsSelected, buildConflictChoicesForPositions, conflictChoiceKey, conflictReviewDecision, type ConflictSelection } from './wikiConflictResolution'
import { usesCrossPageConflictFallback, wikiComparisonBeforeContent } from './wikiConflictComparison'
import { normalizeWikiReviewSelection, toggleAllWikiReviews, toggleWikiReview, wikiReviewBatchSelectionState } from './wikiReviewSelection'

const props = defineProps<{ modelValue: boolean; knowledgeBaseId: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void; (e: 'count-change', value: number): void; (e: 'published'): void }>()
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
const sets = ref<WikiChangeSet[]>([])
const feedbackSignals = ref<WikiFeedbackSignal[]>([])
const selected = ref<WikiChangeSet | null>(null)
const category = ref<WikiChangeCategory | ''>('')
const comment = ref('')
const loading = ref(false)
const detailLoading = ref(false)
const submitting = ref(false)
const selectedBatchIds = ref<string[]>([])
const overrides = ref<Record<string, { knowledge_type: string; maturity_status: string; content: string; summary: string; applicability_text: string }>>({})
type DecisionChoice = 'existing' | 'feedback' | 'custom'
const decisionChoices = ref<Record<string, DecisionChoice>>({})
const mergeTargetSlug = ref('')
const conflictSelections = ref<ConflictSelection>({})
const categoryOptions: { label: string; value: WikiChangeCategory }[] = [
  { label: '新增', value: 'addition' }, { label: '普通更新', value: 'update' }, { label: '冲突', value: 'conflict' },
  { label: '纠错', value: 'correction' }, { label: '旧版本淘汰', value: 'retirement' }, { label: '合并/重复', value: 'merge_duplicate' },
]
const batchSelection = computed(() => wikiReviewBatchSelectionState(selectedBatchIds.value, sets.value))
const selectedFeedback = computed(() => feedbackSignals.value.find(signal => signal.change_set_id === selected.value?.id))
function feedbackFor(set: WikiChangeSet) { return feedbackSignals.value.find(signal => signal.change_set_id === set.id) }

watch(() => props.modelValue, open => { if (open) load() })
watch(() => props.knowledgeBaseId, id => { if (id) load() }, { immediate: true })

async function load() {
  loading.value = true
  try {
    const [changeSetResult, feedbackResult] = await Promise.allSettled([
      listWikiChangeSets(props.knowledgeBaseId, { status: 'pending', review_level: 'L1', change_category: category.value, limit: 100 }),
      listWikiFeedbackSignals(props.knowledgeBaseId, { limit: 200 }),
    ])
    if (changeSetResult.status === 'rejected') throw changeSetResult.reason

    const res: any = changeSetResult.value
    sets.value = res.change_sets || []
    selectedBatchIds.value = normalizeWikiReviewSelection(selectedBatchIds.value, sets.value)
    feedbackSignals.value = feedbackResult.status === 'fulfilled'
      ? ((feedbackResult.value as any).signals || [])
      : []
    emit('count-change', Number(res.total || 0))
    if (selected.value && !sets.value.some(item => item.id === selected.value?.id)) selected.value = null
  } catch (error: any) {
    MessagePlugin.error(error?.message || '加载待审核变更失败')
  } finally { loading.value = false }
}

async function select(set: WikiChangeSet) {
  detailLoading.value = true
  comment.value = ''
  try {
    const res: any = await getWikiChangeSet(props.knowledgeBaseId, set.id)
    selected.value = res.change_set
    conflictSelections.value = {}
    mergeTargetSlug.value = String(selected.value?.items?.[0]?.after?.page_metadata?.possible_duplicate_slug || '')
    overrides.value = {}
    decisionChoices.value = {}
    for (const item of selected.value?.items || []) {
      overrides.value[item.id] = {
        knowledge_type: String(item.after?.knowledge_type || ''),
        maturity_status: String(item.after?.maturity_status || 'pending_review'),
        content: String(item.after?.content || ''),
        summary: String(item.after?.summary || ''),
        applicability_text: JSON.stringify(item.after?.applicability || {}, null, 2),
      }
    }
  } catch (error: any) {
    MessagePlugin.error(error?.message || '加载变更详情失败')
  } finally { detailLoading.value = false }
}

function toggleBatch(id: string, checked: boolean) {
  selectedBatchIds.value = toggleWikiReview(selectedBatchIds.value, sets.value, id, checked)
}

function toggleAllBatch(checked: boolean) {
  selectedBatchIds.value = toggleAllWikiReviews(selectedBatchIds.value, sets.value, checked)
}

async function submitBatch() {
  const batchIds = normalizeWikiReviewSelection(selectedBatchIds.value, sets.value)
  if (!batchIds.length) return
  selectedBatchIds.value = batchIds
  submitting.value = true
  try {
    const res: any = await batchReviewWikiChangeSets(props.knowledgeBaseId, {
      change_set_ids: batchIds,
      decision: 'approved',
      comment: '审核队列批量确认新增内容',
    })
    const succeeded = Number(res.succeeded || 0)
    if (succeeded < batchIds.length) MessagePlugin.warning(`已发布 ${succeeded} 条，其余内容请单独检查`)
    else MessagePlugin.success(`已批量发布 ${succeeded} 条新增内容`)
    selectedBatchIds.value = []
    selected.value = null
    await load()
    if (succeeded > 0) emit('published')
  } catch (error: any) {
    MessagePlugin.error(error?.message || '批量发布失败')
  } finally { submitting.value = false }
}

async function submit(decision: 'approved' | 'rejected', mergeIntoSlug = '') {
  if (!selected.value) return
  if (decision === 'approved' && !isConflict.value && !allDecisionsMade.value) {
    MessagePlugin.warning('请先为每一项选择正确说法')
    return
  }
  if (decision === 'rejected' && !comment.value.trim()) {
    MessagePlugin.warning('请填写拒绝原因')
    return
  }
  submitting.value = true
  try {
    const changedOverrides = Object.fromEntries(Object.entries(overrides.value).map(([id, value]) => {
      const original = selected.value?.items?.find(item => item.id === id)?.after
      const patch: Record<string, any> = {}
      if (value.knowledge_type !== String(original?.knowledge_type || '')) patch.knowledge_type = value.knowledge_type
      if (value.maturity_status !== String(original?.maturity_status || 'pending_review')) patch.maturity_status = value.maturity_status
      if (value.content !== String(original?.content || '')) patch.content = value.content
      if (value.summary !== String(original?.summary || '')) patch.summary = value.summary
      return [id, patch]
    }).filter(([, patch]) => Object.keys(patch).length))
    const itemOverrides = decision === 'approved' && Object.keys(changedOverrides).length ? changedOverrides : undefined
    await reviewWikiChangeSet(props.knowledgeBaseId, selected.value.id, { decision, comment: comment.value.trim(), merge_into_slug: mergeIntoSlug.trim() || undefined, item_overrides: itemOverrides })
    MessagePlugin.success(decision === 'approved' ? '已批准并发布' : '已拒绝')
    selected.value = null
    await load()
    if (decision === 'approved') emit('published')
  } catch (error: any) {
    const message = error?.response?.status === 409 ? '正式页面已变更，请刷新后重新审核' : (error?.message || '审核失败')
    MessagePlugin.error(message)
  } finally { submitting.value = false }
}

async function submitConflict() {
  if (!selected.value || !allConflictChoicesSelected.value) {
    MessagePlugin.warning('请为每一处冲突选择要保留的说法')
    return
  }
  const choices = buildConflictChoicesForPositions(conflictAssessments.value, conflictSelections.value)
  const decision = conflictReviewDecision(choices)
  if (decision === 'rejected' && !comment.value.trim()) comment.value = '保留全部既有有效说法，拒绝当前冲突候选'
  submitting.value = true
  try {
    await reviewWikiChangeSet(props.knowledgeBaseId, selected.value.id, {
      decision, comment: comment.value.trim(), resolution: 'per_conflict', conflict_choices: choices,
    })
    MessagePlugin.success('逐项冲突裁决已保存并执行')
    selected.value = null
    await load()
    if (decision === 'approved') emit('published')
  } catch (error: any) {
    MessagePlugin.error(error?.message || '提交冲突裁决失败')
  } finally { submitting.value = false }
}

async function submitWholeConflict(decision: 'approved' | 'rejected') {
  if (!selected.value || conflictAssessments.value.length > 0) return
  if (decision === 'rejected' && !comment.value.trim()) comment.value = '历史冲突审核无可逐项裁决位置，整单拒绝并保留既有知识'
  submitting.value = true
  try {
    await reviewWikiChangeSet(props.knowledgeBaseId, selected.value.id, {
      decision,
      comment: comment.value.trim(),
      resolution: decision === 'approved' ? 'adopt_candidate' : 'keep_existing',
    })
    MessagePlugin.success(decision === 'approved' ? '已整单批准并发布' : '已整单拒绝')
    selected.value = null
    await load()
    if (decision === 'approved') emit('published')
  } catch (error: any) {
    const message = error?.response?.status === 409 ? '正式页面已变更，请刷新后重新审核' : (error?.message || '提交历史冲突审核失败')
    MessagePlugin.error(message)
  } finally { submitting.value = false }
}

async function deferConflict() {
  if (!selected.value) return
  submitting.value = true
  try {
    await reviewWikiChangeSet(props.knowledgeBaseId, selected.value.id, {
      decision: 'deferred', comment: comment.value.trim(), resolution: 'defer',
    })
    MessagePlugin.success('已记录暂缓意见')
  } catch (error: any) {
    MessagePlugin.error(error?.message || '暂缓审核失败')
  } finally { submitting.value = false }
}

function pageContent(page?: Record<string, any>) { return String(page?.content || page?.summary || '') }

type DiffRow = { text: string; type: 'context' | 'added' | 'removed'; number: number }
type DiffResult = { before: DiffRow[]; after: DiffRow[]; addedLines: number; removedLines: number; changed: boolean; description: string }

function comparisonBefore(item: WikiChangeItem) { return wikiComparisonBeforeContent(item) }
function comparisonAfter(item: WikiChangeItem) { return String(item.after?.content || pageContent(item.after) || '') }
function comparisonBeforeTitle(item: WikiChangeItem) { return usesCrossPageConflictFallback(item) ? '知识库现有说法' : '修改前' }
function comparisonBeforeSubtitle(item: WikiChangeItem) { return usesCrossPageConflictFallback(item) ? '与新内容冲突的已有知识' : '原知识库内容' }
function comparisonAfterTitle(item: WikiChangeItem) { return usesCrossPageConflictFallback(item) ? '反馈建议的新说法' : '修改后' }
function comparisonAfterSubtitle(item: WikiChangeItem) { return usesCrossPageConflictFallback(item) ? '准备发布的新知识内容' : '准备发布的内容' }
function hasAutoContentChange(item: WikiChangeItem) { return comparisonBefore(item) !== comparisonAfter(item) }

function buildDiff(item: WikiChangeItem): DiffResult {
  const before = comparisonBefore(item).split(/\r?\n/)
  const after = comparisonAfter(item).split(/\r?\n/)
  const beforeEmpty = before.length === 1 && !before[0]
  const afterEmpty = after.length === 1 && !after[0]
  const oldLines = beforeEmpty ? [] : before
  const newLines = afterEmpty ? [] : after
  const n = oldLines.length
  const m = newLines.length
  const rowsBefore: DiffRow[] = []
  const rowsAfter: DiffRow[] = []
  let addedLines = 0
  let removedLines = 0

  // A bounded LCS keeps the review drawer responsive even for very large Markdown pages.
  if (n <= 220 && m <= 220) {
    const table = Array.from({ length: n + 1 }, () => new Uint16Array(m + 1))
    for (let i = n - 1; i >= 0; i -= 1) {
      for (let j = m - 1; j >= 0; j -= 1) table[i][j] = oldLines[i] === newLines[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1])
    }
    let i = 0; let j = 0
    while (i < n || j < m) {
      if (i < n && j < m && oldLines[i] === newLines[j]) {
        rowsBefore.push({ text: oldLines[i], type: 'context', number: i + 1 }); rowsAfter.push({ text: newLines[j], type: 'context', number: j + 1 }); i += 1; j += 1
      } else if (j < m && (i === n || table[i][j + 1] >= table[i + 1][j])) {
        rowsAfter.push({ text: newLines[j], type: 'added', number: j + 1 }); addedLines += 1; j += 1
      } else {
        rowsBefore.push({ text: oldLines[i], type: 'removed', number: i + 1 }); removedLines += 1; i += 1
      }
    }
  } else {
    oldLines.forEach((text, index) => { rowsBefore.push({ text, type: 'removed', number: index + 1 }); removedLines += 1 })
    newLines.forEach((text, index) => { rowsAfter.push({ text, type: 'added', number: index + 1 }); addedLines += 1 })
  }

  const changed = addedLines > 0 || removedLines > 0
  const description = !changed
    ? '正文没有变化'
    : oldLines.length === 0
      ? `新增 ${addedLines} 行内容`
      : newLines.length === 0
        ? `删除 ${removedLines} 行内容`
        : `删除 ${removedLines} 行，新增 ${addedLines} 行`
  return { before: rowsBefore, after: rowsAfter, addedLines, removedLines, changed, description }
}

function diffSummary(item: WikiChangeItem) { return buildDiff(item) }
function diffRows(item: WikiChangeItem) {
  const result = buildDiff(item)
  return { before: result.before.filter(row => row.type !== 'added'), after: result.after.filter(row => row.type !== 'removed') }
}

const wikiRelationLabels: Record<string, string> = {
  consistent: '内容一致',
  complementary: '内容互补',
  conflicting: '说法冲突',
  supersedes: '新内容替代旧内容',
  unrelated: '暂无直接关联',
  uncertain: '关系待确认',
  supports: '支持此知识',
  contradicts: '与此知识矛盾',
  tests: '用于验证',
  answers: '解答此问题',
  causes: '可能导致',
  depends_on: '依赖此知识',
  applies_to: '适用于',
  measures: '用于衡量',
  example_of: '属于示例',
  mitigates: '用于降低风险',
}

function escapeWikiHTML(value: string) {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function prepareWikiMarkdown(value: string) {
  const withLinks = value.replace(/\[\[([^\]]+)\]\]/g, (match, inner: string) => {
    const pipeIndex = inner.indexOf('|')
    const slug = (pipeIndex > 0 ? inner.slice(0, pipeIndex) : inner).trim()
    if (!slug) return match
    const fallback = slug.split('/').filter(Boolean).pop() || slug
    const display = (pipeIndex > 0 ? inner.slice(pipeIndex + 1) : fallback).trim() || fallback
    return `<span class="wiki-review-link" title="关联知识：${escapeWikiHTML(slug)}">${escapeWikiHTML(display)}</span>`
  })

  const withCitations = withLinks.replace(/\[c(\d{3,})\]/gi, (_, index: string) => {
    const sourceNumber = Number.parseInt(index, 10) + 1
    return `<sup class="wiki-review-citation" title="来源引用 c${index}">[来源 ${sourceNumber}]</sup>`
  })

  const relationCodes = Object.keys(wikiRelationLabels).join('|')
  return withCitations.replace(new RegExp(`[（(](${relationCodes})[）)]`, 'gi'), (_, relation: string) => {
    const normalized = relation.toLowerCase()
    return `<span class="wiki-relation wiki-relation--${normalized}">${wikiRelationLabels[normalized]}</span>`
  })
}

function renderMarkdown(value?: string) {
  const source = String(value || '').trim()
  if (!source) return '<p>（暂无内容）</p>'
  return sanitizeMarkdownHTML(marked.parse(prepareWikiMarkdown(source), { breaks: true, gfm: true, async: false }) as string)
}
function feedbackDraft(item: WikiChangeItem) {
  return String(item.after?.content || selectedFeedback.value?.suggested_correction || pageContent(item.before))
}
function chooseDecision(item: WikiChangeItem, choice: DecisionChoice) {
  if (isConflict.value) return
  decisionChoices.value = { ...decisionChoices.value, [item.id]: choice }
  const override = overrides.value[item.id]
  if (!override) return
  if (choice === 'existing') override.content = comparisonBefore(item)
  if (choice === 'feedback') override.content = feedbackDraft(item)
  if (choice === 'custom' && !override.content.trim()) override.content = comparisonBefore(item)
}
const allDecisionsMade = computed(() => isConflict.value || Boolean(selected.value?.items?.length && selected.value.items.every(item => decisionChoices.value[item.id])))
function sourceLabel(source?: string) { return ({ mcp: '接口使用反馈', mcp_feedback: '接口使用反馈', manual_correction: '人工纠错', agent_answer: '问答反馈', user_feedback: '用户反馈' } as Record<string, string>)[source || ''] || '系统检查' }
function attentionLabel(risk?: string) { return ({ high: '需要重点确认', medium: '建议仔细确认', low: '常规确认' } as Record<string, string>)[risk || ''] || '常规确认' }
function reasonText(reasons?: string[]) {
  const labels: Record<string, string> = {
    possible_duplicate: '可能与现有知识重复', conflict: '与现有说法不一致', correction: '收到纠错反馈',
    stale: '内容可能已过期', retirement: '建议停止使用', feedback: '收到用户反馈',
  }
  return reasons?.map(reason => labels[reason] || '需要人工确认').join(' / ') || '常规检查'
}
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '' }
function itemTitle(item: WikiChangeItem) {
  return String(item.after?.title || item.before?.title || item.page_slug || '未命名知识')
}
function versionText(item: WikiChangeItem) {
  if (item.operation === 'create') return '发布后为第 1 版'
  const current = Number(item.before?.version || item.expected_version || 0)
  return current > 0 ? `当前第 ${current} 版，发布后为第 ${current + 1} 版` : '发布后会保留为新版本'
}
function conflictTitle(assessment: any) {
  const slug = String(assessment.related_title || assessment.related_slug || '')
  const tail = slug.split('/').filter(Boolean).pop() || '相关知识'
  return tail.replace(/[-_]+/g, ' ')
}
function conflictReason(assessment: any) {
  const reason = String(assessment.reason || '').trim()
  if (!reason) return assessment.applicability_overlap ? '两种说法适用于相同场景，请确认哪一个正确。' : '两种说法适用场景可能不同，请确认是否都需要保留。'
  return `${reason}${/[。！？]$/.test(reason) ? '' : '。'}${assessment.applicability_overlap ? '两种说法适用于相同场景。' : '两种说法的适用场景可能不同。'}`
}
function evidenceText(value: unknown): string {
  if (typeof value === 'string') return value
  if (Array.isArray(value)) return value.map(entry => evidenceText(entry)).filter(Boolean).join('\n\n')
  if (value && typeof value === 'object') {
    const record = value as Record<string, unknown>
    return String(record.excerpt || record.content || record.summary || record.text || Object.values(record).map(entry => evidenceText(entry)).filter(Boolean).join('\n\n'))
  }
  return String(value || '暂无来源内容')
}
function diagnosticText(item: WikiChangeItem) {
  return [`知识路径：${item.page_slug}`, `变更编号：${item.id}`, `来源编号：${item.evidence_chunk_ids?.join(', ') || '无'}`].join('\n')
}
const isConflict = computed(() => selected.value?.change_category === 'conflict')
const conflictAssessments = computed<any[]>(() => {
  return (selected.value?.items || []).flatMap(item => {
    const assessments = item.after?.page_metadata?.cross_page_assessments || []
    const relatedEvidence = item.after?.page_metadata?.related_page_evidence || {}
    return assessments
      .filter((assessment: any) => assessment.relation === 'conflicting' || assessment.relation === 'supersedes')
      .map((assessment: any, assessmentIndex: number) => ({ ...assessment, itemId: item.id, assessmentIndex, relatedEvidence }))
  })
})
const selectedConflictCount = computed(() => buildConflictChoicesForPositions(conflictAssessments.value, conflictSelections.value).length)
const allConflictChoicesSelected = computed(() => allConflictPositionsSelected(conflictAssessments.value, conflictSelections.value))
function conflictSelection(itemId: string, assessmentIndex: number): WikiConflictChoice['resolution'] | undefined {
  return conflictSelections.value[conflictChoiceKey(itemId, assessmentIndex)]
}
function chooseConflict(itemId: string, assessmentIndex: number, resolution: WikiConflictChoice['resolution']) {
  conflictSelections.value = { ...conflictSelections.value, [conflictChoiceKey(itemId, assessmentIndex)]: resolution }
}
function categoryLabel(value: WikiChangeCategory) { return categoryOptions.find(item => item.value === value)?.label || value || '普通更新' }
function categoryTheme(value: WikiChangeCategory) { return value === 'conflict' ? 'danger' : value === 'addition' ? 'success' : value === 'correction' || value === 'retirement' ? 'warning' : 'primary' }
</script>

<style scoped>
.review-workbench { height: calc(100vh - 90px); min-height: 620px; display: flex; flex-direction: column; }
.workflow-header { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: 4px 6px 18px; border-bottom: 1px solid var(--td-component-border); }
.workflow-header h3 { margin: 0; font-size: 18px; }
.workflow-header p { margin: 5px 0 0; color: var(--td-text-color-secondary); font-size: 13px; }
.workflow-steps { display: flex; align-items: center; flex: 0 0 auto; }
.workflow-step { display: flex; align-items: center; gap: 6px; color: var(--td-text-color-placeholder); font-size: 12px; }
.workflow-step b { display: grid; place-items: center; width: 23px; height: 23px; border-radius: 50%; background: var(--td-bg-color-secondarycontainer); }
.workflow-step.done { color: var(--td-success-color); }
.workflow-step.done b { color: white; background: var(--td-success-color); }
.workflow-step.active { color: var(--td-brand-color); font-weight: 600; }
.workflow-step.active b { color: white; background: var(--td-brand-color); box-shadow: 0 0 0 4px var(--td-brand-color-light); }
.workflow-steps i { width: 28px; height: 1px; margin: 0 7px; background: var(--td-component-border); }
.review-layout { display: grid; grid-template-columns: 330px 1fr; flex: 1; min-height: 0; }
.review-list { border-right: 1px solid var(--td-component-border); overflow: auto; padding: 16px 14px 20px 2px; }
.review-filters { display: grid; grid-template-columns: 1fr auto; gap: 8px; margin-bottom: 12px; }
.queue-title { display: flex; align-items: center; justify-content: space-between; margin: 14px 8px 8px; color: var(--td-text-color-secondary); font-size: 12px; font-weight: 600; }
.batch-selector { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 0 8px 8px; color: var(--td-text-color-secondary); font-size: 12px; }
.batch-actions { display: flex; align-items: center; justify-content: space-between; padding: 8px; margin-bottom: 8px; border-radius: 6px; background: var(--td-brand-color-light); font-size: 12px; }
.review-row { width: 100%; margin-bottom: 7px; border: 1px solid var(--td-component-border); background: var(--td-bg-color-container); border-radius: 10px; padding: 12px; text-align: left; cursor: pointer; color: inherit; transition: .18s ease; }
.review-row:hover { transform: translateY(-1px); border-color: var(--td-brand-color-5); box-shadow: 0 5px 16px rgba(0,0,0,.05); }
.review-row.active { background: var(--td-brand-color-light); border-color: var(--td-brand-color); box-shadow: inset 3px 0 0 var(--td-brand-color); }
.review-row-top { display: flex; align-items: center; gap: 8px; font-weight: 600; }
.queue-feedback { display: -webkit-box; overflow: hidden; margin: 8px 0; color: var(--td-text-color-secondary); font-size: 12px; line-height: 1.5; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
.review-row-meta { display: flex; justify-content: space-between; }
.review-row-meta, .detail-meta, .evidence { margin-top: 8px; color: var(--td-text-color-secondary); font-size: 12px; }
.review-detail { padding: 20px 6px 40px 24px; overflow: auto; }
.detail-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 20px; padding-bottom: 18px; }
.detail-head h3 { margin: 4px 0 8px; font-size: 22px; }
.eyebrow { color: var(--td-brand-color); font-size: 12px; font-weight: 600; letter-spacing: .08em; }
.detail-meta { display: flex; align-items: center; gap: 8px; }
.version-result { display: flex; flex-direction: column; min-width: 170px; padding: 11px 14px; border: 1px solid var(--td-success-color-3); border-radius: 9px; background: var(--td-success-color-1); }
.version-result span, .version-result small { color: var(--td-text-color-secondary); font-size: 11px; }
.version-result strong { margin: 3px 0; color: var(--td-success-color); font-size: 13px; }
.affected-notice { display: flex; align-items: center; gap: 12px; margin: 0 0 14px; padding: 11px 14px; border: 1px solid var(--td-brand-color-3); border-radius: 9px; background: var(--td-brand-color-light); }
.affected-notice strong { color: var(--td-brand-color); }
.affected-notice span { color: var(--td-text-color-secondary); font-size: 13px; }
.feedback-context, .attribution-card, .change-item, .publish-card { margin: 0 0 14px; padding: 17px; border: 1px solid var(--td-component-border); border-radius: 11px; background: var(--td-bg-color-container); }
.feedback-context { border-color: var(--td-brand-color-3); background: linear-gradient(135deg, var(--td-brand-color-light), var(--td-bg-color-container) 60%); }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 14px; }
.section-heading > div:first-child { display: flex; align-items: flex-start; gap: 10px; }
.section-heading strong { font-size: 15px; }
.section-heading p { margin: 3px 0 0; color: var(--td-text-color-secondary); font-size: 12px; }
.section-index { display: grid; place-items: center; width: 28px; height: 28px; border-radius: 8px; color: var(--td-brand-color); background: var(--td-brand-color-light); font-size: 11px; font-weight: 700; }
.feedback-markdown { margin: 0 0 12px; padding: 13px 15px; border-left: 3px solid var(--td-brand-color); border-radius: 0 8px 8px 0; background: var(--td-bg-color-container); }
.feedback-badges { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; }
.feedback-conflict { margin: 8px 0; padding: 8px 10px; border-radius: 6px; color: var(--td-warning-color); background: var(--td-warning-color-1); }
.feedback-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.feedback-fields > div { min-width: 0; padding: 10px; border-radius: 6px; background: var(--td-bg-color-container); }
.feedback-fields .suggestion { grid-column: 1 / -1; background: var(--td-warning-color-1); }
.feedback-fields span { color: var(--td-text-color-secondary); font-size: 12px; }
.feedback-fields .markdown-content { margin-top: 6px; }
.attribution-grid { display: grid; grid-template-columns: 1.4fr .8fr 1fr; gap: 10px; }
.attribution-grid > div { display: flex; flex-direction: column; min-width: 0; padding: 12px; border-radius: 8px; background: var(--td-bg-color-secondarycontainer); }
.attribution-grid span, .attribution-grid small { color: var(--td-text-color-secondary); font-size: 11px; }
.attribution-grid strong { overflow: hidden; margin: 5px 0; text-overflow: ellipsis; white-space: nowrap; }
.section-title, .diff-title { font-weight: 600; margin-bottom: 8px; }
.field-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px; }
.review-edit-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 12px; }
.wide-field { grid-column: 1 / -1; }
.change-summary { margin: 0 0 16px; padding: 13px; border: 1px solid var(--td-component-border); border-radius: 10px; background: var(--td-bg-color-secondarycontainer); }
.change-summary-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 11px; }
.change-summary-head > div:first-child { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.change-summary-head strong { font-size: 14px; }
.change-summary-head span { color: var(--td-text-color-secondary); font-size: 12px; }
.change-counts { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 7px; }
.change-count { padding: 3px 8px; border-radius: 999px; font-size: 11px !important; font-weight: 600; white-space: nowrap; }
.change-count.removed { color: var(--td-error-color); background: var(--td-error-color-light); }
.change-count.added { color: var(--td-success-color); background: var(--td-success-color-light); }
.diff-board { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.diff-board-column { min-width: 0; overflow: hidden; border: 1px solid var(--td-component-border); border-radius: 8px; background: var(--td-bg-color-container); }
.diff-board-column--removed { border-color: color-mix(in srgb, var(--td-error-color) 25%, var(--td-component-border)); }
.diff-board-column--added { border-color: color-mix(in srgb, var(--td-success-color) 30%, var(--td-component-border)); }
.diff-board-title { display: flex; align-items: center; gap: 7px; padding: 8px 10px; border-bottom: 1px solid var(--td-component-border); font-size: 12px; }
.diff-board-title small { margin-left: auto; color: var(--td-text-color-placeholder); font-size: 11px; }
.diff-dot { width: 8px; height: 8px; border-radius: 50%; }
.diff-dot.removed { background: var(--td-error-color); }
.diff-dot.added { background: var(--td-success-color); }
.diff-rendered-content { min-height: 300px; max-height: 430px; overflow: auto; padding: 16px 18px; }
.diff-rendered-content :deep(.wiki-review-link) { display: inline-flex; align-items: center; padding: 1px 7px; border: 1px solid var(--td-brand-color-3); border-radius: 999px; color: var(--td-brand-color); background: var(--td-brand-color-light); font-size: .92em; }
.diff-source-details { margin-top: 10px; }
.diff-source-details > summary { display: inline-flex; align-items: center; gap: 6px; color: var(--td-text-color-secondary); cursor: pointer; font-size: 12px; user-select: none; }
.diff-source-details > summary:hover { color: var(--td-brand-color); }
.diff-source-details[open] > summary { margin-bottom: 10px; }
.diff-board--source { margin-top: 0; }
.diff-lines { max-height: 300px; overflow: auto; padding: 5px 0; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.55; }
.diff-line { display: grid; grid-template-columns: 34px 18px minmax(0, 1fr); min-height: 20px; padding: 1px 9px 1px 0; }
.diff-line code { min-width: 0; overflow-wrap: anywhere; white-space: pre-wrap; color: var(--td-text-color-primary); font-family: inherit; }
.diff-line-number { padding-right: 7px; color: var(--td-text-color-placeholder); text-align: right; user-select: none; }
.diff-line-marker { color: var(--td-text-color-placeholder); text-align: center; font-weight: 700; }
.diff-line--removed { background: var(--td-error-color-light); }
.diff-line--removed .diff-line-marker, .diff-line--removed code { color: var(--td-error-color); }
.diff-line--added { background: var(--td-success-color-light); }
.diff-line--added .diff-line-marker, .diff-line--added code { color: var(--td-success-color); }
.diff-empty, .diff-unchanged { padding: 12px; color: var(--td-text-color-placeholder); font-size: 12px; }
.diff-unchanged { border: 1px dashed var(--td-component-border); border-radius: 8px; background: var(--td-bg-color-container); }
.decision-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; margin-bottom: 16px; }
.decision-option { position: relative; min-width: 0; min-height: 142px; padding: 14px; color: inherit; text-align: left; cursor: pointer; border: 2px solid var(--td-component-border); border-radius: 10px; background: var(--td-bg-color-secondarycontainer); transition: .18s ease; }
.decision-option:hover { border-color: var(--td-brand-color-5); transform: translateY(-1px); }
.decision-option.selected { border-color: var(--td-brand-color); background: var(--td-brand-color-light); box-shadow: 0 4px 14px rgba(0,0,0,.05); }
.decision-option:disabled { cursor: not-allowed; opacity: .72; transform: none; }
.decision-option > strong { position: absolute; top: 13px; right: 13px; color: var(--td-brand-color); font-size: 12px; }
.decision-label { display: block; padding-right: 58px; font-weight: 650; }
.decision-preview { display: -webkit-box; overflow: hidden; margin-top: 12px; color: var(--td-text-color-secondary); font-size: 13px; -webkit-line-clamp: 5; -webkit-box-orient: vertical; }
.decision-unavailable { margin: 12px 0 0; color: var(--td-text-color-secondary); font-size: 12px; line-height: 1.55; }
.final-editor { padding: 14px; border: 1px solid var(--td-brand-color-3); border-radius: 10px; background: var(--td-bg-color-container); }
.final-editor .diff-title small { color: var(--td-text-color-secondary); font-weight: 400; }
.choice-hint { padding: 13px 14px; border: 1px dashed var(--td-component-border); border-radius: 9px; color: var(--td-text-color-secondary); background: var(--td-bg-color-secondarycontainer); font-size: 13px; }
.final-preview { min-height: 90px; max-height: 360px; overflow: auto; }
.technical-summary { display: flex; flex-wrap: wrap; gap: 8px 18px; margin-bottom: 10px; }
.conflict-box { margin: 0 0 14px; padding: 17px; border: 1px solid var(--td-error-color-4); border-radius: 11px; background: var(--td-error-color-1); }
.conflict-assessment { margin: 12px 0; padding: 12px; border-radius: 6px; background: var(--td-bg-color-container); }
.conflict-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.claim-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 10px; }
.claim-grid span, .conflict-reason { color: var(--td-text-color-secondary); font-size: 12px; }
.claim-option { position: relative; min-width: 0; padding: 10px; text-align: left; color: inherit; cursor: pointer; border: 2px solid transparent; border-radius: 8px; background: var(--td-bg-color-secondarycontainer); }
.claim-option:hover { border-color: var(--td-brand-color-light); }
.claim-option.selected { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.claim-option > span { display: block; margin-bottom: 6px; }
.claim-option > strong { position: absolute; top: 8px; right: 10px; color: var(--td-brand-color); font-size: 12px; }
.claim-content { max-height: 230px; overflow: auto; padding-top: 8px; }
.conflict-reason { margin: 10px 0; }
.conflict-progress { margin-top: 12px; color: var(--td-warning-color); font-size: 13px; }
.conflict-progress.complete { color: var(--td-success-color); }
.conflict-empty { padding: 12px; border-radius: 6px; background: var(--td-warning-color-light); color: var(--td-warning-color); line-height: 1.6; }
.merge-box { margin: 14px 0; padding: 13px 14px; border: 1px solid var(--td-component-border); border-radius: 9px; }
.merge-controls { display: grid; grid-template-columns: 1fr auto; gap: 10px; }
.diff-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.diff-column { min-width: 0; }
.diff-title { display: flex; align-items: center; justify-content: space-between; }
.content-preview { min-height: 236px; max-height: 430px; overflow: auto; padding: 13px; white-space: pre-wrap; word-break: break-word; border: 1px solid var(--td-component-border); border-radius: 8px; background: var(--td-bg-color-secondarycontainer); font-size: 13px; line-height: 1.7; }
.candidate-editor :deep(textarea) { line-height: 1.7; font-family: inherit; }
pre { margin: 0; padding: 12px; max-height: 340px; overflow: auto; white-space: pre-wrap; word-break: break-word; border-radius: 6px; background: var(--td-bg-color-secondarycontainer); font-size: 12px; }
.review-actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 14px; }
.review-note { margin-top: 4px; color: var(--td-text-color-secondary); font-size: 12px; }
.review-note summary { margin-bottom: 10px; cursor: pointer; }
.evidence-excerpt { margin-top: 10px; }
.technical-details { margin-top: 12px; color: var(--td-text-color-secondary); font-size: 12px; }
.technical-details summary { cursor: pointer; user-select: none; }
.technical-details[open] summary { margin-bottom: 10px; color: var(--td-text-color-primary); }
.diagnostic-details { margin-top: 10px; }
.markdown-content { min-width: 0; color: var(--td-text-color-primary); font-size: 14px; line-height: 1.72; overflow-wrap: anywhere; }
.markdown-content :deep(> :first-child) { margin-top: 0; }
.markdown-content :deep(> :last-child) { margin-bottom: 0; }
.markdown-content :deep(p) { margin: 8px 0; }
.markdown-content :deep(h1), .markdown-content :deep(h2), .markdown-content :deep(h3), .markdown-content :deep(h4) { margin: 16px 0 8px; line-height: 1.4; }
.markdown-content :deep(h1) { font-size: 20px; }
.markdown-content :deep(h2) { font-size: 17px; }
.markdown-content :deep(h3), .markdown-content :deep(h4) { font-size: 15px; }
.markdown-content :deep(ul), .markdown-content :deep(ol) { margin: 8px 0; padding-left: 22px; }
.markdown-content :deep(li) { margin: 4px 0; }
.markdown-content :deep(blockquote) { margin: 10px 0; padding: 8px 12px; border-left: 3px solid var(--td-brand-color-4); color: var(--td-text-color-secondary); background: var(--td-bg-color-secondarycontainer); }
.markdown-content :deep(pre) { margin: 10px 0; }
.markdown-content :deep(code:not(pre code)) { padding: 1px 5px; border-radius: 4px; background: var(--td-bg-color-secondarycontainer); font-size: .92em; }
.markdown-content :deep(table) { width: 100%; margin: 10px 0; border-collapse: collapse; }
.markdown-content :deep(th), .markdown-content :deep(td) { padding: 7px 9px; border: 1px solid var(--td-component-border); text-align: left; }
.markdown-content :deep(th) { background: var(--td-bg-color-secondarycontainer); }
.markdown-content :deep(a) { color: var(--td-brand-color); }
.markdown-content :deep(.wiki-review-link) { display: inline-flex; align-items: center; max-width: 100%; padding: 1px 7px; border: 1px solid var(--td-brand-color-3); border-radius: 5px; color: var(--td-brand-color); background: var(--td-brand-color-1); font-weight: 500; vertical-align: baseline; }
.markdown-content :deep(.wiki-review-link)::before { content: '关联知识'; margin-right: 5px; color: var(--td-text-color-secondary); font-size: 11px; font-weight: 400; }
.markdown-content :deep(.wiki-review-citation) { margin: 0 2px; color: var(--td-brand-color); font-size: 11px; }
.markdown-content :deep(.wiki-relation) { display: inline-flex; align-items: center; margin-left: 5px; padding: 1px 7px; border-radius: 999px; color: var(--td-text-color-secondary); background: var(--td-bg-color-secondarycontainer); font-size: 11px; line-height: 1.7; vertical-align: middle; }
.markdown-content :deep(.wiki-relation--consistent), .markdown-content :deep(.wiki-relation--supports) { color: var(--td-success-color); background: var(--td-success-color-light); }
.markdown-content :deep(.wiki-relation--conflicting), .markdown-content :deep(.wiki-relation--contradicts) { color: var(--td-error-color); background: var(--td-error-color-light); }
.markdown-content :deep(.wiki-relation--uncertain) { color: var(--td-warning-color); background: var(--td-warning-color-light); }
.review-empty, .review-loading { display: grid; place-items: center; min-height: 180px; color: var(--td-text-color-placeholder); }
@media (max-width: 1000px) { .workflow-header { align-items: flex-start; flex-direction: column; } .review-layout { grid-template-columns: 290px 1fr; } .workflow-step span { display: none; } .attribution-grid { grid-template-columns: 1fr; } }
@media (max-width: 760px) { .review-layout { grid-template-columns: 1fr; } .review-list { max-height: 240px; border-right: 0; border-bottom: 1px solid var(--td-component-border); } .diff-grid, .feedback-fields, .decision-grid, .diff-board { grid-template-columns: 1fr; } .change-summary-head { align-items: flex-start; flex-direction: column; } .change-counts { justify-content: flex-start; } .detail-head { flex-direction: column; } }
</style>
