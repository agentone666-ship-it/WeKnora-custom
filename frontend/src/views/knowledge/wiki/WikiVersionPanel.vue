<template>
  <t-drawer v-model:visible="visible" header="版本管理" size="760px" :footer="false" destroy-on-close>
    <div class="version-toolbar">
      <span class="version-page-title">{{ page.title }}</span>
      <div class="version-toolbar-actions">
        <t-button variant="outline" :loading="loading" @click="load">刷新</t-button>
        <t-button v-if="canEdit" theme="primary" :loading="submitting" @click="createDraft">创建草稿</t-button>
      </div>
    </div>

    <t-alert theme="info" message="Agent 检索默认使用当前已发布版本；草稿只有发布后才会影响索引和下游查询。" />
    <div v-if="loading" class="version-loading"><t-loading /></div>
    <div v-else-if="versions.length === 0" class="version-empty">暂无版本记录</div>
    <div v-else class="version-list">
      <article v-for="version in versions" :key="version.id" class="version-card">
        <div class="version-card-head">
          <div>
            <strong>v{{ version.version }}</strong>
            <t-tag size="small" :theme="statusTheme(version.state)">{{ statusLabel(version.state) }}</t-tag>
          </div>
          <span>{{ formatTime(version.created_at) }}</span>
        </div>
        <p v-if="version.change_summary" class="version-summary">{{ version.change_summary }}</p>
        <div class="version-meta">
          <span>创建人：{{ version.created_by || '系统' }}</span>
          <span v-if="version.published_at">发布于 {{ formatTime(version.published_at) }}</span>
        </div>
        <div class="version-actions">
          <t-button v-if="canEdit && version.state === 'draft'" size="small" theme="primary" variant="outline"
            @click="beginEdit(version)">{{ editDraftId === version.id ? '收起编辑' : '编辑草稿' }}</t-button>
          <t-button size="small" variant="text" @click="toggleSnapshot(version)">
            {{ expandedId === version.id ? '收起快照' : '查看快照' }}
          </t-button>
          <t-button v-if="compareBaseId && compareBaseId !== String(version.version)" size="small" variant="text"
            :loading="diffLoading" @click="compareWith(version)">与所选版本对比</t-button>
          <t-button v-else size="small" variant="text" @click="compareBaseId = String(version.version)">选为对比基准</t-button>
          <template v-if="canEdit">
            <t-button v-if="version.state === 'draft'" size="small" theme="primary" variant="outline"
              :loading="submitting" @click="publish(version)">发布</t-button>
            <t-button v-if="version.state === 'history' || version.state === 'archived'" size="small" theme="warning" variant="outline"
              :loading="submitting" @click="rollback(version)">回滚到此版本</t-button>
            <t-button v-if="version.state === 'draft' || version.state === 'history'" size="small" theme="default" variant="outline"
              :loading="submitting" @click="archive(version)">归档</t-button>
          </template>
        </div>
        <div v-if="editDraftId === version.id" class="version-editor">
          <label>标题</label>
          <t-input v-model="draftForm.title" placeholder="节点标题" />
          <label>摘要</label>
          <t-textarea v-model="draftForm.summary" :autosize="{ minRows: 2, maxRows: 5 }" placeholder="节点摘要" />
          <div class="version-content-head">
            <label>正文（Markdown）</label>
            <div class="version-content-tabs" role="tablist" aria-label="正文显示方式">
              <button type="button" role="tab" :aria-selected="draftContentView === 'edit'"
                :class="{ active: draftContentView === 'edit' }" @click="setDraftContentView('edit')">Markdown</button>
              <button type="button" role="tab" :aria-selected="draftContentView === 'preview'"
                :class="{ active: draftContentView === 'preview' }" @click="setDraftContentView('preview')">可视化编辑</button>
            </div>
          </div>
          <t-textarea v-if="draftContentView === 'edit'" v-model="draftForm.content"
            :autosize="{ minRows: 10, maxRows: 24 }" placeholder="节点正文" />
          <div v-else :ref="setPreviewEditorRef" class="version-markdown-preview" contenteditable="true"
            role="textbox" aria-multiline="true" aria-label="可视化编辑 Wiki 正文" spellcheck="true"
            @input="syncPreviewToMarkdown"></div>
          <label>本次修改说明</label>
          <t-input v-model="draftForm.changeSummary" placeholder="例如：补充营业时间例外规则" />
          <div class="version-editor-actions">
            <t-button variant="outline" @click="editDraftId = ''">取消</t-button>
            <t-button theme="primary" :loading="submitting" @click="saveDraft(version)">保存草稿</t-button>
          </div>
        </div>
        <pre v-if="expandedId === version.id" class="version-json">{{ pretty(version.snapshot) }}</pre>
      </article>
    </div>

    <section v-if="diffResult" class="version-diff">
      <div class="version-diff-head">
        <strong>版本差异</strong>
        <t-button size="small" variant="text" @click="diffResult = null">关闭</t-button>
      </div>
      <pre>{{ pretty(diffResult.changes ?? diffResult) }}</pre>
    </section>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { marked } from 'marked'
import { MessagePlugin } from 'tdesign-vue-next'
import { sanitizeMarkdownHTML } from '@/utils/security'
import {
  archiveWikiPageVersion,
  createWikiPageDraft,
  diffWikiPageVersions,
  listWikiPageVersions,
  publishWikiPageVersion,
  rollbackWikiPageVersion,
  updateWikiPageDraft,
  type WikiPage,
  type WikiPageVersion,
  type WikiPageVersionDiff,
} from '@/api/wiki'

const props = defineProps<{ modelValue: boolean; knowledgeBaseId: string; page: WikiPage; canEdit?: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void; (e: 'changed'): void }>()
const visible = ref(props.modelValue)
const loading = ref(false)
const submitting = ref(false)
const diffLoading = ref(false)
const versions = ref<WikiPageVersion[]>([])
const expandedId = ref('')
const compareBaseId = ref('')
const diffResult = ref<WikiPageVersionDiff | null>(null)
const editDraftId = ref('')
const draftContentView = ref<'edit' | 'preview'>('edit')
const previewEditorRef = ref<HTMLElement | null>(null)
const draftForm = ref({ title: '', summary: '', content: '', changeSummary: '' })
const draftPreviewHTML = computed(() => {
  if (!draftForm.value.content.trim()) return '<p class="empty-preview">暂无正文内容</p>'
  // Wiki pages contain two pieces of domain syntax that plain Markdown does
  // not understand. Render them as readable labels in preview instead of
  // leaking storage syntax such as [[entity/foo|名称]] and [c001].
  const preprocessed = draftForm.value.content
    .replace(/\[\[([^\]]+)\]\]/g, (_, inner: string) => {
      const pipeIndex = inner.indexOf('|')
      const slug = pipeIndex > 0 ? inner.slice(0, pipeIndex).trim() : inner.trim()
      const display = pipeIndex > 0 ? inner.slice(pipeIndex + 1).trim() : slug.split('/').pop() || slug
      return `<span class="wiki-preview-link" data-slug="${slug}" title="关联节点：${slug}">${display}</span>`
    })
    .replace(/\[c(\d{3,})\]/gi, (_, index: string) => {
      const sourceNumber = Number.parseInt(index, 10) + 1
      return `<sup class="wiki-preview-citation" data-chunk-id="c${index}" title="来源引用 c${index}">[来源 ${sourceNumber}]</sup>`
    })
  const html = marked.parse(preprocessed, { breaks: true, async: false }) as string
  return sanitizeMarkdownHTML(html)
})

function setPreviewEditorRef(element: unknown) {
  previewEditorRef.value = element instanceof HTMLElement ? element : null
}

async function setDraftContentView(view: 'edit' | 'preview') {
  if (draftContentView.value === view) return
  draftContentView.value = view
  if (view === 'preview') {
    await nextTick()
    if (previewEditorRef.value) previewEditorRef.value.innerHTML = draftPreviewHTML.value
  }
}

function childMarkdown(element: Element): string {
  return Array.from(element.childNodes).map(nodeToMarkdown).join('')
}

function nodeToMarkdown(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) return node.textContent || ''
  if (!(node instanceof HTMLElement)) return ''
  const tag = node.tagName.toLowerCase()
  const body = childMarkdown(node)
  if (node.classList.contains('wiki-preview-link')) {
    const slug = node.dataset.slug || node.title.replace(/^关联节点：/, '')
    return `[[${slug}|${node.textContent || slug}]]`
  }
  if (node.classList.contains('wiki-preview-citation')) {
    return `[${node.dataset.chunkId || node.title.replace(/^来源引用\s*/, '')}]`
  }
  if (/^h[1-6]$/.test(tag)) return `${'#'.repeat(Number(tag[1]))} ${body.trim()}\n\n`
  if (tag === 'p' || tag === 'div') return `${body.trim()}\n\n`
  if (tag === 'br') return '\n'
  if (tag === 'strong' || tag === 'b') return `**${body}**`
  if (tag === 'em' || tag === 'i') return `*${body}*`
  if (tag === 'del' || tag === 's') return `~~${body}~~`
  if (tag === 'code' && node.parentElement?.tagName.toLowerCase() !== 'pre') return `\`${node.textContent || ''}\``
  if (tag === 'pre') return `\n\`\`\`\n${node.textContent || ''}\n\`\`\`\n\n`
  if (tag === 'blockquote') return `${body.trim().split('\n').map(line => `> ${line}`).join('\n')}\n\n`
  if (tag === 'li') return body.trim()
  if (tag === 'ul' || tag === 'ol') {
    return `${Array.from(node.children).map((item, index) => `${tag === 'ol' ? `${index + 1}.` : '-'} ${nodeToMarkdown(item)}`).join('\n')}\n\n`
  }
  if (tag === 'a') return `[${body}](${node.getAttribute('href') || ''})`
  if (tag === 'img') return `![${node.getAttribute('alt') || ''}](${node.getAttribute('src') || ''})`
  if (tag === 'hr') return '\n---\n\n'
  return body
}

function syncPreviewToMarkdown(event: Event) {
  const editor = event.currentTarget as HTMLElement
  draftForm.value.content = Array.from(editor.childNodes)
    .map(nodeToMarkdown)
    .join('')
    .replace(/\n{3,}/g, '\n\n')
    .trimEnd()
}

watch(() => props.modelValue, value => { visible.value = value; if (value) void load() })
watch(visible, value => emit('update:modelValue', value))
watch(() => props.page.id, () => { versions.value = []; compareBaseId.value = ''; diffResult.value = null; editDraftId.value = ''; if (visible.value) void load() })

function unwrap<T>(response: any): T { return (response?.data ?? response) as T }
async function load() {
  loading.value = true
  try {
    const data = unwrap<any>(await listWikiPageVersions(props.knowledgeBaseId, props.page.id))
    versions.value = (Array.isArray(data) ? data : data?.versions ?? []).sort(
      (a: WikiPageVersion, b: WikiPageVersion) => b.version - a.version,
    )
  } catch (error: any) { MessagePlugin.error(error?.message || '加载版本历史失败') }
  finally { loading.value = false }
}
async function mutate(action: () => Promise<any>, success: string) {
  submitting.value = true
  try { await action(); MessagePlugin.success(success); await load(); emit('changed') }
  catch (error: any) { MessagePlugin.error(error?.message || '版本操作失败') }
  finally { submitting.value = false }
}
async function createDraft() {
  submitting.value = true
  try {
    const created = unwrap<WikiPageVersion>(await createWikiPageDraft(props.knowledgeBaseId, props.page.id, { snapshot: props.page, change_summary: '从当前已发布版本创建草稿' }))
    MessagePlugin.success('草稿已创建，可以开始编辑')
    await load()
    beginEdit(versions.value.find(item => item.id === created.id) || created)
  } catch (error: any) { MessagePlugin.error(error?.message || '创建草稿失败') }
  finally { submitting.value = false }
}
function beginEdit(version: WikiPageVersion) {
  if (editDraftId.value === version.id) { editDraftId.value = ''; return }
  const snapshot = version.snapshot || {}
  editDraftId.value = version.id
  draftContentView.value = 'edit'
  draftForm.value = {
    title: String(snapshot.title || ''),
    summary: String(snapshot.summary || ''),
    content: String(snapshot.content || ''),
    changeSummary: version.change_summary || '',
  }
}
async function saveDraft(version: WikiPageVersion) {
  const snapshot = { ...version.snapshot, title: draftForm.value.title.trim(), summary: draftForm.value.summary, content: draftForm.value.content }
  if (!snapshot.title) { MessagePlugin.warning('标题不能为空'); return }
  await mutate(() => updateWikiPageDraft(props.knowledgeBaseId, props.page.id, String(version.version), {
    snapshot,
    change_summary: draftForm.value.changeSummary.trim(),
  }), '草稿已保存，发布前不会影响当前节点和 Agent 检索')
  editDraftId.value = ''
}
function publish(version: WikiPageVersion) { return mutate(() => publishWikiPageVersion(props.knowledgeBaseId, props.page.id, String(version.version)), '版本已发布') }
function rollback(version: WikiPageVersion) { return mutate(() => rollbackWikiPageVersion(props.knowledgeBaseId, props.page.id, String(version.version)), `已回滚到 v${version.version}`) }
function archive(version: WikiPageVersion) { return mutate(() => archiveWikiPageVersion(props.knowledgeBaseId, props.page.id, String(version.version)), '版本已归档') }
async function compareWith(version: WikiPageVersion) {
  diffLoading.value = true
  try { diffResult.value = unwrap(await diffWikiPageVersions(props.knowledgeBaseId, props.page.id, compareBaseId.value, String(version.version))) }
  catch (error: any) { MessagePlugin.error(error?.message || '版本对比失败') }
  finally { diffLoading.value = false }
}
function toggleSnapshot(version: WikiPageVersion) { expandedId.value = expandedId.value === version.id ? '' : version.id }
function pretty(value: unknown) { return JSON.stringify(value, null, 2) }
function formatTime(value?: string) { return value ? new Date(value).toLocaleString() : '-' }
function statusLabel(status: string) { return ({ draft: '草稿', published: '已发布', history: '历史版本', archived: '已归档' } as Record<string, string>)[status] || status }
function statusTheme(status: string): 'primary' | 'success' | 'default' { return status === 'published' ? 'success' : status === 'draft' ? 'primary' : 'default' }
</script>

<style scoped lang="less">
.version-toolbar,.version-card-head,.version-actions,.version-meta,.version-diff-head{display:flex;align-items:center}.version-toolbar,.version-card-head,.version-diff-head{justify-content:space-between}.version-toolbar{margin-bottom:16px}.version-page-title{font-weight:600}.version-toolbar-actions,.version-actions,.version-meta,.version-card-head>div{display:flex;gap:8px;align-items:center}.version-loading,.version-empty{padding:48px;text-align:center;color:var(--td-text-color-secondary)}.version-list{display:flex;flex-direction:column;gap:12px;margin-top:16px}.version-card{padding:16px;border:1px solid var(--td-component-stroke);border-radius:8px}.version-card-head{color:var(--td-text-color-secondary);font-size:12px}.version-card-head strong{font-size:16px;color:var(--td-text-color-primary)}.version-summary{margin:10px 0;color:var(--td-text-color-primary)}.version-meta{font-size:12px;color:var(--td-text-color-placeholder)}.version-actions{margin-top:10px;flex-wrap:wrap}.version-json,.version-diff pre{max-height:360px;padding:12px;overflow:auto;background:var(--td-bg-color-secondarycontainer);border-radius:6px;font-size:12px;white-space:pre-wrap}.version-diff{margin-top:20px;padding-top:16px;border-top:1px solid var(--td-component-stroke)}
.version-editor{display:flex;flex-direction:column;gap:8px;margin-top:14px;padding:14px;background:var(--td-bg-color-secondarycontainer);border-radius:8px}.version-editor label{font-size:13px;font-weight:600}.version-editor-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:4px}
.version-content-head{display:flex;align-items:center;justify-content:space-between}.version-content-tabs{display:flex;padding:2px;background:var(--td-bg-color-component);border-radius:6px}.version-content-tabs button{padding:4px 12px;border:0;border-radius:4px;background:transparent;color:var(--td-text-color-secondary);font-size:12px;cursor:pointer}.version-content-tabs button.active{background:var(--td-bg-color-container);color:var(--td-brand-color);box-shadow:var(--td-shadow-1)}
.version-markdown-preview{min-height:240px;max-height:520px;padding:16px;overflow:auto;border:1px solid var(--td-component-border);border-radius:6px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);font-size:14px;line-height:1.75}.version-markdown-preview :deep(h1),.version-markdown-preview :deep(h2),.version-markdown-preview :deep(h3),.version-markdown-preview :deep(h4){margin:16px 0 8px;line-height:1.35}.version-markdown-preview :deep(h1:first-child),.version-markdown-preview :deep(h2:first-child),.version-markdown-preview :deep(h3:first-child){margin-top:0}.version-markdown-preview :deep(p){margin:8px 0}.version-markdown-preview :deep(ul),.version-markdown-preview :deep(ol){padding-left:24px}.version-markdown-preview :deep(blockquote){margin:12px 0;padding:8px 12px;border-left:4px solid var(--td-brand-color);background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary)}.version-markdown-preview :deep(code){padding:2px 4px;border-radius:4px;background:var(--td-bg-color-secondarycontainer);font-family:var(--app-font-family-mono)}.version-markdown-preview :deep(pre){padding:12px;overflow:auto;border-radius:6px;background:var(--td-bg-color-secondarycontainer)}.version-markdown-preview :deep(pre code){padding:0;background:transparent}.version-markdown-preview :deep(table){width:100%;border-collapse:collapse}.version-markdown-preview :deep(th),.version-markdown-preview :deep(td){padding:8px;border:1px solid var(--td-component-stroke)}.version-markdown-preview :deep(a){color:var(--td-brand-color)}.version-markdown-preview :deep(img){max-width:100%;height:auto}.version-markdown-preview :deep(.empty-preview){color:var(--td-text-color-placeholder)}
.version-markdown-preview :deep(.wiki-preview-link){color:var(--td-brand-color);font-weight:500}.version-markdown-preview :deep(.wiki-preview-citation){margin-left:2px;color:var(--td-brand-color);font-size:11px;font-weight:500}
.version-markdown-preview[contenteditable="true"]{cursor:text;outline:none}.version-markdown-preview[contenteditable="true"]:focus{border-color:var(--td-brand-color);box-shadow:0 0 0 2px var(--td-brand-color-focus)}
</style>
