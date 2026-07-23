<template>
  <t-drawer v-model:visible="visible" header="反馈驱动的知识更新" size="1180px" :footer="false" destroy-on-close>
    <div class="review-workbench">
      <header class="workflow-header">
        <div>
          <h3>反馈审核工作台</h3>
          <p>从反馈信号定位知识节点，生成修正候选，检测冲突并发布为可回滚的新版本。</p>
        </div>
        <div class="workflow-steps" aria-label="反馈处理流程">
          <div class="workflow-step done"><b>1</b><span>信号入库</span></div>
          <i></i>
          <div class="workflow-step done"><b>2</b><span>归因定位</span></div>
          <i></i>
          <div class="workflow-step active"><b>3</b><span>审核修正</span></div>
          <i></i>
          <div class="workflow-step"><b>4</b><span>发布验证</span></div>
        </div>
      </header>
      <div class="review-layout">
      <aside class="review-list">
        <div class="review-filters">
          <t-select v-model="category" :options="categoryOptions" clearable placeholder="全部反馈类型" @change="load" />
          <t-button variant="outline" :loading="loading" @click="load">刷新</t-button>
        </div>
        <div v-if="batchSelection.total" class="batch-selector">
          <t-checkbox :checked="batchSelection.checked" :indeterminate="batchSelection.indeterminate"
            @change="toggleAllBatch">
            全选当前可批量审核项（{{ batchSelection.total }}）
          </t-checkbox>
          <span v-if="conflictCount">{{ conflictCount }} 条冲突需逐条裁决</span>
        </div>
        <div v-if="selectedBatchIds.length" class="batch-actions">
          <span>已选 {{ selectedBatchIds.length }} 条非冲突变更</span>
          <t-button size="small" theme="primary" :loading="submitting" @click="submitBatch">批量批准</t-button>
        </div>
        <div class="queue-title"><span>待处理信号</span><t-tag size="small" theme="primary">{{ sets.length }}</t-tag></div>
        <div v-if="!loading && sets.length === 0" class="review-empty">暂无待处理反馈</div>
        <div v-for="set in sets" :key="set.id" class="review-row" :class="{ active: selected?.id === set.id }"
          @click="select(set)">
          <div class="review-row-top">
            <t-checkbox v-if="set.change_category !== 'conflict'" :checked="selectedBatchIds.includes(set.id)"
              @click.stop @change="toggleBatch(set.id, $event)" />
            <t-tag size="small" :theme="categoryTheme(set.change_category)">{{ categoryLabel(set.change_category) }}</t-tag>
            <span>{{ set.items?.[0]?.after?.title || set.items?.[0]?.page_slug || '未命名节点' }}</span>
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
              <span class="eyebrow">待审核知识更新</span>
              <h3>{{ selected.items?.[0]?.after?.title || selected.items?.[0]?.page_slug }}</h3>
              <div class="detail-meta">
                <t-tag :theme="categoryTheme(selected.change_category)">{{ categoryLabel(selected.change_category) }}</t-tag>
                <t-tag variant="light-outline">{{ selected.review_level || 'L1' }} 人工审核</t-tag>
                <t-tag v-if="selectedFeedback" :theme="riskTheme(selectedFeedback.risk_level)" variant="light">{{ riskLabel(selectedFeedback.risk_level) }}</t-tag>
              </div>
            </div>
            <div class="version-result"><span>审核通过后</span><strong>发布为节点新版本</strong><small>可在版本历史中回滚</small></div>
          </div>

          <section v-if="selectedFeedback" class="feedback-context">
            <div class="section-heading">
              <div><span class="section-index">01</span><div><strong>反馈信号</strong><p>审核为什么需要修改这条知识</p></div></div>
              <div class="feedback-badges">
                <t-tag size="small" theme="primary">{{ sourceLabel(selectedFeedback.source) }}</t-tag>
                <t-tag size="small" variant="light-outline">置信度 {{ formatConfidence(selectedFeedback.confidence) }}</t-tag>
              </div>
            </div>
            <blockquote>{{ selectedFeedback.feedback_text }}</blockquote>
            <div class="feedback-fields compact">
              <div v-if="selectedFeedback.original_question"><span>触发问题</span><p>{{ selectedFeedback.original_question }}</p></div>
              <div v-if="selectedFeedback.answer_excerpt"><span>原回答</span><p>{{ selectedFeedback.answer_excerpt }}</p></div>
              <div v-if="selectedFeedback.suggested_correction" class="suggestion"><span>建议修正方向</span><p>{{ selectedFeedback.suggested_correction }}</p></div>
            </div>
          </section>

          <section class="attribution-card">
            <div class="section-heading">
              <div><span class="section-index">02</span><div><strong>归因定位</strong><p>反馈已关联到对应知识节点和来源记录</p></div></div>
            </div>
            <div class="attribution-grid">
              <div><span>目标节点</span><strong>{{ selected.items?.[0]?.after?.title || selected.items?.[0]?.page_slug }}</strong><small>{{ selected.items?.[0]?.page_slug }}</small></div>
              <div><span>归因结果</span><strong>{{ selectedFeedback ? formatConfidence(selectedFeedback.confidence) : '系统候选' }}</strong><small>{{ selectedFeedback?.attributed_node_ids?.length || selected.items?.length || 0 }} 个关联节点</small></div>
              <div><span>处理策略</span><strong>{{ selectedFeedback?.strategy === 'auto_update' ? '自动更新' : '人工审核' }}</strong><small>{{ reasonText(selected.reasons) }}</small></div>
            </div>
          </section>

          <section v-if="isConflict" class="conflict-box">
            <div class="section-heading"><div><span class="section-index">03</span><div><strong>冲突检测</strong><p>逐项选择最终保留的知识口径</p></div></div></div>
            <div v-if="conflictAssessments.length === 0" class="conflict-empty">
              这是一条历史冲突审核，但快照中没有可逐项裁决的冲突位置。请在下方整单批准、拒绝或暂缓。
            </div>
            <div v-for="assessment in conflictAssessments" :key="`${assessment.itemId}:${assessment.assessmentIndex}`" class="conflict-assessment">
              <div class="conflict-head">
                <strong>{{ assessment.related_slug }}</strong>
                <t-tag theme="danger" size="small">置信度 {{ formatConfidence(assessment.confidence) }}</t-tag>
              </div>
              <div class="claim-grid">
                <button type="button" class="claim-option"
                  :class="{ selected: conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'adopt_candidate' }"
                  @click="chooseConflict(assessment.itemId, assessment.assessmentIndex, 'adopt_candidate')">
                  <span>候选新说法</span>
                  <strong v-if="conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'adopt_candidate'">✓ 保留这个说法</strong>
                  <pre>{{ assessment.candidate_claim || '未提取' }}</pre>
                </button>
                <button type="button" class="claim-option"
                  :class="{ selected: conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'keep_existing' }"
                  @click="chooseConflict(assessment.itemId, assessment.assessmentIndex, 'keep_existing')">
                  <span>既有说法</span>
                  <strong v-if="conflictSelection(assessment.itemId, assessment.assessmentIndex) === 'keep_existing'">✓ 保留这个说法</strong>
                  <pre>{{ assessment.existing_claim || '未提取' }}</pre>
                </button>
              </div>
              <div class="conflict-reason">冲突位置：{{ assessment.reason }}；适用范围{{ assessment.applicability_overlap ? '重叠' : '不重叠' }}</div>
              <details v-if="assessment.relatedEvidence[assessment.related_slug]" class="technical-details">
                <summary>查看既有页面证据与来源</summary>
                <pre>{{ pretty(assessment.relatedEvidence[assessment.related_slug]) }}</pre>
              </details>
            </div>
            <div v-if="conflictAssessments.length" class="conflict-progress" :class="{ complete: allConflictChoicesSelected }">
              已选择 {{ selectedConflictCount }}/{{ conflictAssessments.length }} 处；每一处都选择后才能提交
            </div>
          </section>
          <section v-for="item in selected.items" :key="item.id" class="change-item">
            <div class="section-heading">
              <div><span class="section-index">{{ isConflict ? '04' : '03' }}</span><div><strong>修正候选</strong><p>{{ operationLabel(item.operation) }} {{ item.page_slug }}</p></div></div>
              <div class="field-tags"><t-tag v-for="field in visibleChangedFields(item.changed_fields)" :key="field" size="small" variant="light-outline">{{ fieldLabel(field) }}</t-tag></div>
            </div>
            <div v-if="overrides[item.id]" class="review-edit-fields">
              <t-select v-model="overrides[item.id].knowledge_type" :options="knowledgeTypeOptions" placeholder="知识类型" />
              <t-select v-model="overrides[item.id].maturity_status" :options="maturityOptions" placeholder="成熟度" />
            </div>
            <div class="diff-grid">
              <div class="diff-column">
                <div class="diff-title"><span>当前已发布内容</span><t-tag size="small" variant="light-outline">旧版本</t-tag></div>
                <div class="content-preview">{{ pageContent(item.before) || '（当前没有正文）' }}</div>
              </div>
              <div class="diff-column">
                <div class="diff-title"><span>候选修正版</span><t-tag size="small" theme="success">可编辑</t-tag></div>
                <t-textarea v-if="overrides[item.id]" v-model="overrides[item.id].content" class="candidate-editor" :autosize="{ minRows: 10, maxRows: 22 }" placeholder="编辑审核通过后要发布的正文" />
              </div>
            </div>
            <details class="technical-details">
              <summary>查看溯源证据与技术详情（{{ item.evidence_chunk_ids?.length || 0 }} 条）</summary>
              <div class="evidence">证据 Chunk：{{ item.evidence_chunk_ids?.join(', ') || '无' }}</div>
              <div v-for="(excerpt, chunkId) in item.evidence_excerpts || {}" :key="chunkId" class="evidence-excerpt"><div class="diff-title">Chunk {{ chunkId }}</div><pre>{{ excerpt }}</pre></div>
              <div class="evidence-excerpt"><div class="diff-title">完整字段变更</div><pre>{{ pretty({ before: item.before, after: item.after }) }}</pre></div>
            </details>
          </section>

          <div v-if="selected.items?.[0]?.operation === 'create' && !isConflict" class="merge-box">
            <t-input v-model="mergeTargetSlug" placeholder="合并到已有卡片 slug（可选）" />
            <t-button variant="outline" :disabled="!mergeTargetSlug.trim()" :loading="submitting" @click="submit('approved', mergeTargetSlug)">合并并批准</t-button>
          </div>
          <section class="publish-card">
            <div class="section-heading"><div><span class="section-index">{{ isConflict ? '05' : '04' }}</span><div><strong>审核与发布</strong><p>批准后更新目标节点，并创建可追溯的新版本</p></div></div></div>
            <t-textarea v-model="comment" :autosize="{ minRows: 3, maxRows: 6 }" placeholder="填写审核意见；拒绝时必须说明原因" />
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
              <t-button theme="danger" variant="outline" :loading="submitting" @click="submit('rejected')">拒绝候选</t-button>
              <t-button theme="primary" :loading="submitting" @click="submit('approved')">审核通过并发布新版本</t-button>
            </template>
          </div>
          </section>
        </template>
        <div v-else class="review-empty">从左侧选择一条反馈，开始归因与修正审核</div>
      </main>
      </div>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { batchReviewWikiChangeSets, getWikiChangeSet, listWikiChangeSets, listWikiFeedbackSignals, reviewWikiChangeSet, type WikiChangeCategory, type WikiChangeItem, type WikiChangeSet, type WikiConflictChoice, type WikiFeedbackSignal } from '@/api/wiki'
import { normalizeWikiReviewSelection, toggleAllWikiReviews, toggleWikiReview, wikiReviewBatchSelectionState } from './wikiReviewSelection'
import { allConflictPositionsSelected, buildConflictChoicesForPositions, conflictChoiceKey, conflictReviewDecision, type ConflictSelection } from './wikiConflictResolution'

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
const mergeTargetSlug = ref('')
const conflictSelections = ref<ConflictSelection>({})
const categoryOptions: { label: string; value: WikiChangeCategory }[] = [
  { label: '新增', value: 'addition' }, { label: '普通更新', value: 'update' }, { label: '冲突', value: 'conflict' },
  { label: '纠错', value: 'correction' }, { label: '旧版本淘汰', value: 'retirement' }, { label: '合并/重复', value: 'merge_duplicate' },
]
const knowledgeTypeOptions = ['knowledge', 'experience', 'question', 'hypothesis', 'experiment', 'metric', 'procedure', 'failure', 'case', 'rule'].map(value => ({ label: value, value }))
const maturityOptions = ['draft', 'pending_review', 'partially_verified', 'verified', 'disputed', 'outdated', 'unsupported', 'archived'].map(value => ({ label: value, value }))
const batchSelection = computed(() => wikiReviewBatchSelectionState(selectedBatchIds.value, sets.value))
const conflictCount = computed(() => sets.value.filter(set => set.change_category === 'conflict').length)
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
    feedbackSignals.value = feedbackResult.status === 'fulfilled'
      ? ((feedbackResult.value as any).signals || [])
      : []
    selectedBatchIds.value = normalizeWikiReviewSelection(selectedBatchIds.value, sets.value)
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

async function submit(decision: 'approved' | 'rejected', mergeIntoSlug = '') {
  if (!selected.value) return
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
    const res: any = await batchReviewWikiChangeSets(props.knowledgeBaseId, { change_set_ids: batchIds, decision: 'approved', comment: '前端 L1 批量审核' })
    const succeeded = Number(res.succeeded || 0)
    if (succeeded < batchIds.length) MessagePlugin.warning(`已批准 ${succeeded} 条，其余项目请查看返回原因`)
    else MessagePlugin.success(`已批准 ${succeeded} 条 L1 变更`)
    selectedBatchIds.value = []
    await load()
    if (succeeded > 0) emit('published')
  } catch (error: any) {
    MessagePlugin.error(error?.message || '批量审核失败')
  } finally { submitting.value = false }
}

function pretty(value: unknown) { return value ? JSON.stringify(value, null, 2) : '（无，这是新建页面）' }
function pageContent(page?: Record<string, any>) { return String(page?.content || page?.summary || '') }
function visibleChangedFields(fields?: string[]) { return (fields || []).filter(field => ['content', 'summary', 'title', 'knowledge_type', 'maturity_status'].includes(field)) }
function fieldLabel(field: string) { return ({ content: '正文', summary: '摘要', title: '标题', knowledge_type: '知识类型', maturity_status: '成熟度' } as Record<string, string>)[field] || field }
function sourceLabel(source?: string) { return ({ mcp: 'MCP 调用', mcp_feedback: 'MCP 反馈', manual_correction: '人工纠错', agent_answer: 'Agent 回答', user_feedback: '用户反馈' } as Record<string, string>)[source || ''] || source || '系统检查' }
function riskLabel(risk?: string) { return ({ high: '高风险', medium: '中风险', low: '低风险' } as Record<string, string>)[risk || ''] || '待评估' }
function riskTheme(risk?: string) { return risk === 'high' ? 'danger' : risk === 'low' ? 'success' : 'warning' }
function reasonText(reasons?: string[]) { return reasons?.join(' / ') || '常规检查' }
function operationLabel(operation: WikiChangeItem['operation']) { return ({ create: '新建', update: '更新', archive: '归档' } as const)[operation] }
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '' }
function impactText(page?: Record<string, any>) {
  if (!page) return '未知'
  return [page.business_line, ...(page.scenario_ids || []), ...(page.affected_metrics || [])].filter(Boolean).join(' / ') || '全局或待归类'
}
function systemSuggestion(set: WikiChangeSet) {
  if (set.change_category === 'conflict') return '对照双方说法、证据和适用范围，明确选择最终保留版本'
  if (set.reasons?.includes('possible_duplicate')) return '优先合并到已有卡片，避免重复口径'
  return '确认分类与场景后可批准'
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
function formatConfidence(value: number) { return `${Math.round(Number(value || 0) * 100)}%` }
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
.feedback-context, .attribution-card, .change-item, .publish-card { margin: 0 0 14px; padding: 17px; border: 1px solid var(--td-component-border); border-radius: 11px; background: var(--td-bg-color-container); }
.feedback-context { border-color: var(--td-brand-color-3); background: linear-gradient(135deg, var(--td-brand-color-light), var(--td-bg-color-container) 60%); }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 14px; }
.section-heading > div:first-child { display: flex; align-items: flex-start; gap: 10px; }
.section-heading strong { font-size: 15px; }
.section-heading p { margin: 3px 0 0; color: var(--td-text-color-secondary); font-size: 12px; }
.section-index { display: grid; place-items: center; width: 28px; height: 28px; border-radius: 8px; color: var(--td-brand-color); background: var(--td-brand-color-light); font-size: 11px; font-weight: 700; }
.feedback-context blockquote { margin: 0 0 12px; padding: 13px 15px; border-left: 3px solid var(--td-brand-color); border-radius: 0 8px 8px 0; background: var(--td-bg-color-container); font-size: 14px; line-height: 1.7; }
.feedback-badges { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; }
.feedback-conflict { margin: 8px 0; padding: 8px 10px; border-radius: 6px; color: var(--td-warning-color); background: var(--td-warning-color-1); }
.feedback-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.feedback-fields > div { min-width: 0; padding: 10px; border-radius: 6px; background: var(--td-bg-color-container); }
.feedback-fields .suggestion { grid-column: 1 / -1; background: var(--td-warning-color-1); }
.feedback-fields span { color: var(--td-text-color-secondary); font-size: 12px; }
.feedback-fields p { margin: 6px 0 0; white-space: pre-wrap; word-break: break-word; }
.attribution-grid { display: grid; grid-template-columns: 1.4fr .8fr 1fr; gap: 10px; }
.attribution-grid > div { display: flex; flex-direction: column; min-width: 0; padding: 12px; border-radius: 8px; background: var(--td-bg-color-secondarycontainer); }
.attribution-grid span, .attribution-grid small { color: var(--td-text-color-secondary); font-size: 11px; }
.attribution-grid strong { overflow: hidden; margin: 5px 0; text-overflow: ellipsis; white-space: nowrap; }
.section-title, .diff-title { font-weight: 600; margin-bottom: 8px; }
.field-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px; }
.review-edit-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 12px; }
.wide-field { grid-column: 1 / -1; }
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
.claim-option pre { padding: 8px 0 0; background: transparent; }
.conflict-reason { margin: 10px 0; }
.conflict-progress { margin-top: 12px; color: var(--td-warning-color); font-size: 13px; }
.conflict-progress.complete { color: var(--td-success-color); }
.conflict-empty { padding: 12px; border-radius: 6px; background: var(--td-warning-color-light); color: var(--td-warning-color); line-height: 1.6; }
.merge-box { display: grid; grid-template-columns: 1fr auto; gap: 10px; margin: 14px 0; }
.diff-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.diff-column { min-width: 0; }
.diff-title { display: flex; align-items: center; justify-content: space-between; }
.content-preview { min-height: 236px; max-height: 430px; overflow: auto; padding: 13px; white-space: pre-wrap; word-break: break-word; border: 1px solid var(--td-component-border); border-radius: 8px; background: var(--td-bg-color-secondarycontainer); font-size: 13px; line-height: 1.7; }
.candidate-editor :deep(textarea) { line-height: 1.7; font-family: inherit; }
pre { margin: 0; padding: 12px; max-height: 340px; overflow: auto; white-space: pre-wrap; word-break: break-word; border-radius: 6px; background: var(--td-bg-color-secondarycontainer); font-size: 12px; }
.review-actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 14px; }
.evidence-excerpt { margin-top: 10px; }
.technical-details { margin-top: 12px; color: var(--td-text-color-secondary); font-size: 12px; }
.technical-details summary { cursor: pointer; user-select: none; }
.technical-details[open] summary { margin-bottom: 10px; color: var(--td-text-color-primary); }
.review-empty, .review-loading { display: grid; place-items: center; min-height: 180px; color: var(--td-text-color-placeholder); }
@media (max-width: 1000px) { .workflow-header { align-items: flex-start; flex-direction: column; } .review-layout { grid-template-columns: 290px 1fr; } .workflow-step span { display: none; } .attribution-grid { grid-template-columns: 1fr; } }
@media (max-width: 760px) { .review-layout { grid-template-columns: 1fr; } .review-list { max-height: 240px; border-right: 0; border-bottom: 1px solid var(--td-component-border); } .diff-grid, .feedback-fields { grid-template-columns: 1fr; } .detail-head { flex-direction: column; } }
</style>
