<template>
  <t-drawer v-model:visible="visible" header="Wiki 变更审核" size="920px" :footer="false" destroy-on-close>
    <div class="review-layout">
      <aside class="review-list">
        <div class="review-filters">
          <t-select v-model="category" :options="categoryOptions" clearable placeholder="变更分类" @change="load" />
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
        <div v-if="!loading && sets.length === 0" class="review-empty">暂无待审核变更</div>
        <div v-for="set in sets" :key="set.id" class="review-row" :class="{ active: selected?.id === set.id }"
          @click="select(set)">
          <div class="review-row-top">
            <t-checkbox v-if="set.change_category !== 'conflict'" :checked="selectedBatchIds.includes(set.id)"
              @click.stop @change="toggleBatch(set.id, $event)" />
            <t-tag size="small" :theme="categoryTheme(set.change_category)">{{ categoryLabel(set.change_category) }}</t-tag>
            <span>{{ set.items?.[0]?.after?.title || set.items?.[0]?.page_slug || '未命名变更' }}</span>
          </div>
          <div class="review-row-meta">{{ formatTime(set.created_at) }} · {{ reasonText(set.reasons) }}</div>
        </div>
      </aside>

      <main class="review-detail">
        <div v-if="detailLoading" class="review-loading"><t-loading /></div>
        <template v-else-if="selected">
          <div class="detail-head">
            <div>
              <h3>{{ selected.items?.[0]?.after?.title || selected.items?.[0]?.page_slug }}</h3>
              <div class="detail-meta">
                <t-tag :theme="categoryTheme(selected.change_category)">{{ categoryLabel(selected.change_category) }}</t-tag>
                <t-tag variant="light-outline">需人工审核</t-tag>
                <span>触发原因：{{ reasonText(selected.reasons) }}</span>
              </div>
            </div>
          </div>

          <section v-if="isConflict" class="conflict-box">
            <div class="section-title">冲突定位与版本取舍</div>
            <div v-for="assessment in conflictAssessments" :key="assessment.related_slug" class="conflict-assessment">
              <div class="conflict-head">
                <strong>{{ assessment.related_slug }}</strong>
                <t-tag theme="danger" size="small">置信度 {{ formatConfidence(assessment.confidence) }}</t-tag>
              </div>
              <div class="claim-grid">
                <div><span>候选新说法</span><pre>{{ assessment.candidate_claim || '未提取' }}</pre></div>
                <div><span>既有说法</span><pre>{{ assessment.existing_claim || '未提取' }}</pre></div>
              </div>
              <div class="conflict-reason">冲突位置：{{ assessment.reason }}；适用范围{{ assessment.applicability_overlap ? '重叠' : '不重叠' }}</div>
              <details v-if="relatedEvidence[assessment.related_slug]">
                <summary>查看既有页面证据与来源</summary>
                <pre>{{ pretty(relatedEvidence[assessment.related_slug]) }}</pre>
              </details>
            </div>
            <t-radio-group v-model="conflictResolution" class="resolution-options">
              <t-radio value="keep_existing">保留既有说法，拒绝新候选</t-radio>
              <t-radio value="adopt_candidate">采用新说法，淘汰冲突旧版本</t-radio>
              <t-radio value="edit_candidate">人工编辑后采用，并淘汰冲突旧版本</t-radio>
              <t-radio value="split_scope">适用范围不同，拆分后同时保留</t-radio>
              <t-radio value="defer">暂不裁决，继续待审核</t-radio>
            </t-radio-group>
            <t-textarea v-model="retainedClaim" :autosize="{ minRows: 2, maxRows: 5 }" placeholder="最终保留的标准说法（将写入审核日志）" />
          </section>
          <div class="review-context-grid">
            <div><span>资料性质</span><strong>{{ selected.items?.[0]?.after?.page_metadata?.document_nature || 'unknown' }}</strong></div>
            <div><span>来源版本时间</span><strong>{{ formatTime(selected.items?.[0]?.after?.page_metadata?.source_updated_at || selected.created_at) }}</strong></div>
            <div><span>影响范围</span><strong>{{ impactText(selected.items?.[0]?.after) }}</strong></div>
            <div><span>系统建议</span><strong>{{ systemSuggestion(selected) }}</strong></div>
          </div>

          <section v-for="item in selected.items" :key="item.id" class="change-item">
            <div class="section-title">{{ operationLabel(item.operation) }} · {{ item.page_slug }}</div>
            <div class="field-tags">
              <t-tag v-for="field in item.changed_fields" :key="field" size="small" variant="light-outline">{{ field }}</t-tag>
            </div>
            <div v-if="overrides[item.id]" class="review-edit-fields">
              <t-select v-model="overrides[item.id].knowledge_type" :options="knowledgeTypeOptions" placeholder="知识类型" />
              <t-select v-model="overrides[item.id].maturity_status" :options="maturityOptions" placeholder="成熟度" />
              <t-textarea v-if="conflictResolution === 'edit_candidate'" v-model="overrides[item.id].content" class="wide-field" :autosize="{ minRows: 4, maxRows: 10 }" placeholder="人工修订后的正文" />
              <t-input v-if="conflictResolution === 'edit_candidate'" v-model="overrides[item.id].summary" class="wide-field" placeholder="人工修订后的摘要" />
              <t-textarea v-if="conflictResolution === 'split_scope'" v-model="overrides[item.id].applicability_text" class="wide-field" :autosize="{ minRows: 3, maxRows: 8 }" placeholder='请输入拆分后的适用范围 JSON，例如 {"scope":"华东区"}' />
            </div>
            <div class="diff-grid">
              <div class="diff-column">
                <div class="diff-title">旧值</div>
                <pre>{{ pretty(item.before) }}</pre>
              </div>
              <div class="diff-column">
                <div class="diff-title">候选新值</div>
                <pre>{{ pretty(item.after) }}</pre>
              </div>
            </div>
            <div class="evidence">证据 Chunk：{{ item.evidence_chunk_ids?.join(', ') || '无（不可发布为正式知识）' }}</div>
            <div v-for="(excerpt, chunkId) in item.evidence_excerpts || {}" :key="chunkId" class="evidence-excerpt">
              <div class="diff-title">Chunk {{ chunkId }} 原文</div>
              <pre>{{ excerpt }}</pre>
            </div>
          </section>

          <div v-if="selected.items?.[0]?.operation === 'create' && !isConflict" class="merge-box">
            <t-input v-model="mergeTargetSlug" placeholder="合并到已有卡片 slug（可选）" />
            <t-button variant="outline" :disabled="!mergeTargetSlug.trim()" :loading="submitting" @click="submit('approved', mergeTargetSlug)">合并并批准</t-button>
          </div>
          <t-textarea v-model="comment" :autosize="{ minRows: 3, maxRows: 6 }" placeholder="审核意见（拒绝时建议必填）" />
          <div class="review-actions">
            <template v-if="isConflict">
              <t-button variant="outline" :loading="submitting" @click="submitConflict">提交冲突裁决</t-button>
            </template>
            <template v-else>
              <t-button theme="danger" variant="outline" :loading="submitting" @click="submit('rejected')">拒绝</t-button>
              <t-button theme="primary" :loading="submitting" @click="submit('approved')">批准并发布</t-button>
            </template>
          </div>
        </template>
        <div v-else class="review-empty">选择一条变更查看字段差异与证据</div>
      </main>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { batchReviewWikiChangeSets, getWikiChangeSet, listWikiChangeSets, reviewWikiChangeSet, type WikiChangeCategory, type WikiChangeItem, type WikiChangeSet, type WikiConflictResolution } from '@/api/wiki'
import { normalizeWikiReviewSelection, toggleAllWikiReviews, toggleWikiReview, wikiReviewBatchSelectionState } from './wikiReviewSelection'

const props = defineProps<{ modelValue: boolean; knowledgeBaseId: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void; (e: 'count-change', value: number): void; (e: 'published'): void }>()
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
const sets = ref<WikiChangeSet[]>([])
const selected = ref<WikiChangeSet | null>(null)
const category = ref<WikiChangeCategory | ''>('')
const comment = ref('')
const loading = ref(false)
const detailLoading = ref(false)
const submitting = ref(false)
const selectedBatchIds = ref<string[]>([])
const overrides = ref<Record<string, { knowledge_type: string; maturity_status: string; content: string; summary: string; applicability_text: string }>>({})
const mergeTargetSlug = ref('')
const conflictResolution = ref<WikiConflictResolution | ''>('')
const retainedClaim = ref('')
const categoryOptions: { label: string; value: WikiChangeCategory }[] = [
  { label: '新增', value: 'addition' }, { label: '普通更新', value: 'update' }, { label: '冲突', value: 'conflict' },
  { label: '纠错', value: 'correction' }, { label: '旧版本淘汰', value: 'retirement' }, { label: '合并/重复', value: 'merge_duplicate' },
]
const knowledgeTypeOptions = ['knowledge', 'experience', 'question', 'hypothesis', 'experiment', 'metric', 'procedure', 'failure', 'case', 'rule'].map(value => ({ label: value, value }))
const maturityOptions = ['draft', 'pending_review', 'partially_verified', 'verified', 'disputed', 'outdated', 'unsupported', 'archived'].map(value => ({ label: value, value }))
const batchSelection = computed(() => wikiReviewBatchSelectionState(selectedBatchIds.value, sets.value))
const conflictCount = computed(() => sets.value.filter(set => set.change_category === 'conflict').length)

watch(() => props.modelValue, open => { if (open) load() })
watch(() => props.knowledgeBaseId, id => { if (id) load() }, { immediate: true })

async function load() {
  loading.value = true
  try {
    const res: any = await listWikiChangeSets(props.knowledgeBaseId, { status: 'pending', review_level: 'L1', change_category: category.value, limit: 100 })
    sets.value = res.change_sets || []
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
    conflictResolution.value = ''
    retainedClaim.value = ''
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
      if (conflictResolution.value === 'split_scope') patch.applicability = JSON.parse(value.applicability_text || '{}')
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
  if (!selected.value || !conflictResolution.value) {
    MessagePlugin.warning('请选择冲突的处理方式')
    return
  }
  if (conflictResolution.value === 'edit_candidate' && !retainedClaim.value.trim()) {
    MessagePlugin.warning('请填写最终保留的标准说法')
    return
  }
  if (conflictResolution.value === 'split_scope') {
    try {
      for (const item of selected.value.items || []) JSON.parse(overrides.value[item.id]?.applicability_text || '{}')
    } catch {
      MessagePlugin.warning('适用范围必须是合法 JSON')
      return
    }
  }
  const decision = conflictResolution.value === 'keep_existing' ? 'rejected' : conflictResolution.value === 'defer' ? 'deferred' : 'approved'
  if (decision === 'rejected' && !comment.value.trim()) comment.value = '保留既有有效说法，拒绝当前冲突候选'
  submitting.value = true
  try {
    const itemOverrides: Record<string, Record<string, any>> = {}
    if (conflictResolution.value === 'edit_candidate' || conflictResolution.value === 'split_scope') {
      for (const item of selected.value.items || []) {
        const value = overrides.value[item.id]
        if (!value) continue
        if (conflictResolution.value === 'edit_candidate') itemOverrides[item.id] = { content: value.content, summary: value.summary }
        else itemOverrides[item.id] = { applicability: JSON.parse(value.applicability_text || '{}') }
      }
    }
    await reviewWikiChangeSet(props.knowledgeBaseId, selected.value.id, {
      decision, comment: comment.value.trim(), resolution: conflictResolution.value,
      retained_claim: retainedClaim.value.trim(), item_overrides: Object.keys(itemOverrides).length ? itemOverrides : undefined,
    })
    MessagePlugin.success(decision === 'deferred' ? '已记录暂缓意见' : '冲突裁决已保存并执行')
    selected.value = null
    await load()
    if (decision === 'approved') emit('published')
  } catch (error: any) {
    MessagePlugin.error(error?.message || '提交冲突裁决失败')
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
const conflictAssessments = computed<any[]>(() => selected.value?.items?.[0]?.after?.page_metadata?.cross_page_assessments || [])
const relatedEvidence = computed<Record<string, any>>(() => selected.value?.items?.[0]?.after?.page_metadata?.related_page_evidence || {})
function categoryLabel(value: WikiChangeCategory) { return categoryOptions.find(item => item.value === value)?.label || value || '普通更新' }
function categoryTheme(value: WikiChangeCategory) { return value === 'conflict' ? 'danger' : value === 'addition' ? 'success' : value === 'correction' || value === 'retirement' ? 'warning' : 'primary' }
function formatConfidence(value: number) { return `${Math.round(Number(value || 0) * 100)}%` }
</script>

<style scoped>
.review-layout { display: grid; grid-template-columns: 300px 1fr; height: calc(100vh - 90px); min-height: 560px; }
.review-list { border-right: 1px solid var(--td-component-border); overflow: auto; padding-right: 12px; }
.review-filters { display: grid; grid-template-columns: 1fr auto; gap: 8px; margin-bottom: 12px; }
.batch-selector { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 0 8px 8px; color: var(--td-text-color-secondary); font-size: 12px; }
.batch-actions { display: flex; align-items: center; justify-content: space-between; padding: 8px; margin-bottom: 8px; border-radius: 6px; background: var(--td-brand-color-light); font-size: 12px; }
.review-row { width: 100%; border: 1px solid transparent; background: transparent; border-radius: 8px; padding: 12px; text-align: left; cursor: pointer; color: inherit; }
.review-row:hover, .review-row.active { background: var(--td-bg-color-container-hover); border-color: var(--td-brand-color-light); }
.review-row-top { display: flex; align-items: center; gap: 8px; font-weight: 600; }
.review-row-meta, .detail-meta, .evidence { margin-top: 8px; color: var(--td-text-color-secondary); font-size: 12px; }
.review-detail { padding: 0 0 24px 20px; overflow: auto; }
.detail-head h3 { margin: 0 0 8px; }
.detail-meta { display: flex; align-items: center; gap: 8px; }
.review-context-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin: 16px 0; }
.review-context-grid > div { display: flex; flex-direction: column; gap: 4px; padding: 10px; border-radius: 6px; background: var(--td-bg-color-secondarycontainer); }
.review-context-grid span { color: var(--td-text-color-secondary); font-size: 12px; }
.change-item { margin: 20px 0; }
.section-title, .diff-title { font-weight: 600; margin-bottom: 8px; }
.field-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px; }
.review-edit-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 12px; }
.wide-field { grid-column: 1 / -1; }
.conflict-box { margin: 16px 0; padding: 14px; border: 1px solid var(--td-error-color-4); border-radius: 8px; background: var(--td-error-color-1); }
.conflict-assessment { margin: 12px 0; padding: 12px; border-radius: 6px; background: var(--td-bg-color-container); }
.conflict-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.claim-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 10px; }
.claim-grid span, .conflict-reason { color: var(--td-text-color-secondary); font-size: 12px; }
.conflict-reason { margin: 10px 0; }
.resolution-options { display: grid; gap: 8px; margin: 14px 0; }
.merge-box { display: grid; grid-template-columns: 1fr auto; gap: 10px; margin: 14px 0; }
.diff-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.diff-column { min-width: 0; }
pre { margin: 0; padding: 12px; max-height: 340px; overflow: auto; white-space: pre-wrap; word-break: break-word; border-radius: 6px; background: var(--td-bg-color-secondarycontainer); font-size: 12px; }
.review-actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 14px; }
.evidence-excerpt { margin-top: 10px; }
.review-empty, .review-loading { display: grid; place-items: center; min-height: 180px; color: var(--td-text-color-placeholder); }
@media (max-width: 900px) { .review-layout { grid-template-columns: 1fr; } .review-list { max-height: 220px; border-right: 0; border-bottom: 1px solid var(--td-component-border); } .diff-grid { grid-template-columns: 1fr; } }
</style>
