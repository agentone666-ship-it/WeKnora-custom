<template>
  <t-drawer v-model:visible="visible" header="Wiki 变更审核" size="920px" :footer="false" destroy-on-close>
    <div class="review-layout">
      <aside class="review-list">
        <div class="review-filters">
          <t-select v-model="level" :options="levelOptions" clearable placeholder="审核级别" @change="load" />
          <t-button variant="outline" :loading="loading" @click="load">刷新</t-button>
        </div>
        <div v-if="selectedBatchIds.length" class="batch-actions">
          <span>已选 {{ selectedBatchIds.length }} 条 L1</span>
          <t-button size="small" theme="primary" :loading="submitting" @click="submitBatch">批量批准</t-button>
        </div>
        <div v-if="!loading && sets.length === 0" class="review-empty">暂无待审核变更</div>
        <div v-for="set in sets" :key="set.id" class="review-row" :class="{ active: selected?.id === set.id }"
          @click="select(set)">
          <div class="review-row-top">
            <t-checkbox v-if="set.review_level === 'L1'" :checked="selectedBatchIds.includes(set.id)"
              @click.stop @change="toggleBatch(set.id, $event)" />
            <t-tag size="small" :theme="set.review_level === 'L2' ? 'danger' : 'warning'">{{ set.review_level }}</t-tag>
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
                <t-tag :theme="selected.review_level === 'L2' ? 'danger' : 'warning'">{{ selected.review_level }}</t-tag>
                <span>触发原因：{{ reasonText(selected.reasons) }}</span>
              </div>
            </div>
          </div>
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

          <div v-if="selected.items?.[0]?.operation === 'create'" class="merge-box">
            <t-input v-model="mergeTargetSlug" placeholder="合并到已有卡片 slug（可选）" />
            <t-button variant="outline" :disabled="!mergeTargetSlug.trim()" :loading="submitting" @click="submit('approved', mergeTargetSlug)">合并并批准</t-button>
          </div>
          <t-textarea v-model="comment" :autosize="{ minRows: 3, maxRows: 6 }" placeholder="审核意见（拒绝时建议必填）" />
          <div class="review-actions">
            <t-button theme="danger" variant="outline" :loading="submitting" @click="submit('rejected')">拒绝</t-button>
            <t-button theme="primary" :loading="submitting" @click="submit('approved')">批准并发布</t-button>
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
import { batchReviewWikiChangeSets, getWikiChangeSet, listWikiChangeSets, reviewWikiChangeSet, type WikiChangeItem, type WikiChangeSet } from '@/api/wiki'

const props = defineProps<{ modelValue: boolean; knowledgeBaseId: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void; (e: 'count-change', value: number): void; (e: 'published'): void }>()
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
const sets = ref<WikiChangeSet[]>([])
const selected = ref<WikiChangeSet | null>(null)
const level = ref('')
const comment = ref('')
const loading = ref(false)
const detailLoading = ref(false)
const submitting = ref(false)
const selectedBatchIds = ref<string[]>([])
const overrides = ref<Record<string, { knowledge_type: string; maturity_status: string }>>({})
const mergeTargetSlug = ref('')
const levelOptions = [{ label: 'L1 轻审核', value: 'L1' }, { label: 'L2 严格审核', value: 'L2' }]
const knowledgeTypeOptions = ['knowledge', 'experience', 'question', 'hypothesis', 'experiment', 'metric', 'procedure', 'failure', 'case', 'rule'].map(value => ({ label: value, value }))
const maturityOptions = ['draft', 'pending_review', 'partially_verified', 'verified', 'disputed', 'outdated', 'unsupported', 'archived'].map(value => ({ label: value, value }))

watch(() => props.modelValue, open => { if (open) load() })
watch(() => props.knowledgeBaseId, id => { if (id) load() }, { immediate: true })

async function load() {
  loading.value = true
  try {
    const res: any = await listWikiChangeSets(props.knowledgeBaseId, { status: 'pending', review_level: level.value, limit: 100 })
    sets.value = res.change_sets || []
    selectedBatchIds.value = selectedBatchIds.value.filter(id => sets.value.some(item => item.id === id && item.review_level === 'L1'))
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
    mergeTargetSlug.value = String(selected.value?.items?.[0]?.after?.page_metadata?.possible_duplicate_slug || '')
    overrides.value = {}
    for (const item of selected.value?.items || []) {
      overrides.value[item.id] = {
        knowledge_type: String(item.after?.knowledge_type || ''),
        maturity_status: String(item.after?.maturity_status || 'pending_review'),
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
    const changedOverrides = Object.fromEntries(Object.entries(overrides.value).filter(([id, value]) => {
      const original = selected.value?.items?.find(item => item.id === id)?.after
      return value.knowledge_type !== String(original?.knowledge_type || '') || value.maturity_status !== String(original?.maturity_status || 'pending_review')
    }))
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

function toggleBatch(id: string, checked: boolean) {
  selectedBatchIds.value = checked ? Array.from(new Set([...selectedBatchIds.value, id])) : selectedBatchIds.value.filter(item => item !== id)
}

async function submitBatch() {
  if (!selectedBatchIds.value.length) return
  submitting.value = true
  try {
    const res: any = await batchReviewWikiChangeSets(props.knowledgeBaseId, { change_set_ids: selectedBatchIds.value, decision: 'approved', comment: '前端 L1 批量审核' })
    const succeeded = Number(res.succeeded || 0)
    if (succeeded < selectedBatchIds.value.length) MessagePlugin.warning(`已批准 ${succeeded} 条，其余项目请查看返回原因`)
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
  if (set.review_level === 'L2') return '逐字段核对原文、来源权威性和适用边界后再发布'
  if (set.reasons?.includes('possible_duplicate')) return '优先合并到已有卡片，避免重复口径'
  return '确认分类与场景后可批准'
}
</script>

<style scoped>
.review-layout { display: grid; grid-template-columns: 300px 1fr; height: calc(100vh - 90px); min-height: 560px; }
.review-list { border-right: 1px solid var(--td-component-border); overflow: auto; padding-right: 12px; }
.review-filters { display: grid; grid-template-columns: 1fr auto; gap: 8px; margin-bottom: 12px; }
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
.merge-box { display: grid; grid-template-columns: 1fr auto; gap: 10px; margin: 14px 0; }
.diff-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.diff-column { min-width: 0; }
pre { margin: 0; padding: 12px; max-height: 340px; overflow: auto; white-space: pre-wrap; word-break: break-word; border-radius: 6px; background: var(--td-bg-color-secondarycontainer); font-size: 12px; }
.review-actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 14px; }
.evidence-excerpt { margin-top: 10px; }
.review-empty, .review-loading { display: grid; place-items: center; min-height: 180px; color: var(--td-text-color-placeholder); }
@media (max-width: 900px) { .review-layout { grid-template-columns: 1fr; } .review-list { max-height: 220px; border-right: 0; border-bottom: 1px solid var(--td-component-border); } .diff-grid { grid-template-columns: 1fr; } }
</style>
