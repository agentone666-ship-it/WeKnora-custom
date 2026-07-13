<template>
  <div class="mcp-logs-page">
    <header class="page-header">
      <div>
        <h1>MCP 调用日志</h1>
        <p>查看外部设备通过 MCP 发起的问答、知识更新和文件上传记录。</p>
      </div>
      <t-button :loading="loading" theme="primary" @click="loadLogs">刷新</t-button>
    </header>

    <section class="summary-grid">
      <div class="summary-card"><span>总调用</span><strong>{{ rows.length }}</strong></div>
      <div class="summary-card"><span>Wiki 问答</span><strong>{{ countTool('ask_wiki') }}</strong></div>
      <div class="summary-card"><span>RAG 问答</span><strong>{{ countTool('ask_rag') }}</strong></div>
      <div class="summary-card"><span>知识写入</span><strong>{{ writeCount }}</strong></div>
    </section>

    <section class="filters">
      <t-input v-model="keyword" clearable placeholder="搜索问题、文件名或 Request ID" />
      <t-select v-model="toolFilter" :options="toolOptions" />
    </section>

    <t-alert v-if="error" theme="error" :message="error" class="state-alert" />
    <div v-else-if="loading && !rows.length" class="loading-state"><t-loading /> 正在加载 MCP 日志…</div>
    <t-empty v-else-if="!filteredRows.length" description="暂无 MCP 调用记录" />

    <div v-else class="log-list">
      <article v-for="row in filteredRows" :key="row.id" class="log-card">
        <div class="log-main">
          <div class="tool-cell">
            <t-tag :theme="toolTheme(row.tool)" variant="light">{{ row.tool }}</t-tag>
            <span class="time">{{ formatTime(row.created_at) }}</span>
          </div>
          <div class="subject">
            <strong>{{ row.subject }}</strong>
            <span class="request-id">{{ row.knowledge_base ? `${row.knowledge_base} · ` : '' }}{{ row.request_id || row.id }}</span>
          </div>
          <div class="metrics">
            <t-tag :theme="row.status === 'success' ? 'success' : 'danger'" variant="light-outline">{{ row.status === 'success' ? '成功' : '失败' }}</t-tag>
            <span v-if="row.duration_ms">{{ (row.duration_ms / 1000).toFixed(1) }}s</span>
            <span v-if="row.steps">{{ row.steps }} 个 Agent 步骤</span>
          </div>
          <t-button variant="text" @click="toggle(row.id)">{{ expanded.has(row.id) ? '收起' : '详情' }}</t-button>
        </div>
        <div v-if="expanded.has(row.id)" class="detail-panel">
          <template v-if="row.answer"><h3>回答结果</h3><pre>{{ row.answer }}</pre></template>
          <template v-if="row.references"><h3>引用来源</h3><pre>{{ pretty(row.references) }}</pre></template>
          <template v-if="row.agent_steps"><h3>Agent Steps</h3><pre>{{ pretty(row.agent_steps) }}</pre></template>
          <template v-if="row.raw"><h3>写入详情</h3><pre>{{ pretty(row.raw) }}</pre></template>
        </div>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getSessionsList, getMessageList } from '@/api/chat'
import { listKnowledgeBases, listKnowledgeFiles } from '@/api/knowledge-base'

type LogRow = { id:string; request_id?:string; tool:string; subject:string; status:string; created_at:string; duration_ms:number; steps:number; knowledge_base?:string; answer?:string; references?:unknown; agent_steps?:unknown; raw?:unknown }
const rows = ref<LogRow[]>([])
const loading = ref(false)
const error = ref('')
const keyword = ref('')
const toolFilter = ref('all')
const expanded = ref(new Set<string>())
const toolOptions = [
  { label: '全部工具', value: 'all' }, { label: 'ask_wiki', value: 'ask_wiki' },
  { label: 'ask_rag', value: 'ask_rag' }, { label: 'update_knowledge', value: 'update_knowledge' },
  { label: 'upload_knowledge_file', value: 'upload_knowledge_file' },
]
const filteredRows = computed(() => rows.value.filter(r => (toolFilter.value === 'all' || r.tool === toolFilter.value) && (!keyword.value || `${r.subject} ${r.request_id || ''}`.toLowerCase().includes(keyword.value.toLowerCase()))))
const writeCount = computed(() => countTool('update_knowledge') + countTool('upload_knowledge_file'))
const countTool = (tool:string) => rows.value.filter(r => r.tool === tool).length
const toggle = (id:string) => { const next = new Set(expanded.value); next.has(id) ? next.delete(id) : next.add(id); expanded.value = next }
const pretty = (value:unknown) => JSON.stringify(value, null, 2)
const formatTime = (v:string) => new Date(v).toLocaleString('zh-CN', { hour12:false })
const toolTheme = (tool:string): any => tool === 'ask_wiki' ? 'primary' : tool === 'ask_rag' ? 'default' : 'warning'

async function loadLogs() {
  loading.value = true; error.value = ''
  try {
    const output: LogRow[] = []
    const sessionResp:any = await getSessionsList(1, 100, 'mcp')
    for (const session of sessionResp?.data || []) {
      const msgResp:any = await getMessageList({ session_id: session.id, limit: 20, created_at: '' })
      const messages:any[] = msgResp?.data || []
      const user = messages.find(m => m.role === 'user')
      const assistant = [...messages].reverse().find(m => m.role === 'assistant')
      if (!user) continue
      const steps = Array.isArray(assistant?.agent_steps) ? assistant.agent_steps.length : 0
      output.push({ id: session.id, request_id: user.request_id || assistant?.request_id, tool: steps ? 'ask_wiki' : 'ask_rag', subject: user.content || '未命名问题', status: !assistant || assistant.is_completed === false ? 'failed' : 'success', created_at: user.created_at || session.created_at, duration_ms: assistant?.agent_duration_ms || 0, steps, answer: assistant?.content, references: assistant?.knowledge_references, agent_steps: assistant?.agent_steps })
    }
    const kbResp:any = await listKnowledgeBases()
    for (const kb of kbResp?.data || []) {
      const knowledgeResp:any = await listKnowledgeFiles(kb.id, { page:1, page_size:100, source:'mcp' })
      for (const item of knowledgeResp?.data || []) {
        output.push({ id:`knowledge-${item.id}`, tool:item.type === 'manual' ? 'update_knowledge' : 'upload_knowledge_file', subject:item.title || item.file_name || '知识写入', status:item.parse_status === 'failed' ? 'failed' : 'success', created_at:item.created_at, duration_ms:0, steps:0, knowledge_base:kb.name, raw:item })
      }
    }
    rows.value = output.sort((a,b) => +new Date(b.created_at) - +new Date(a.created_at))
  } catch (e:any) { error.value = e?.message || '加载 MCP 日志失败' }
  finally { loading.value = false }
}
onMounted(loadLogs)
</script>

<style scoped>
.mcp-logs-page{height:100%;overflow:auto;padding:28px 34px;background:var(--td-bg-color-page,#f5f7fa)}
.page-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:22px}.page-header h1{margin:0;font-size:26px}.page-header p{margin:7px 0 0;color:var(--td-text-color-secondary)}
.summary-grid{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-bottom:18px}.summary-card{padding:18px;background:var(--td-bg-color-container);border:1px solid var(--td-component-border);border-radius:10px;display:flex;flex-direction:column}.summary-card span{color:var(--td-text-color-secondary)}.summary-card strong{font-size:26px;margin-top:5px}
.filters{display:grid;grid-template-columns:minmax(280px,1fr) 220px;gap:12px;margin-bottom:16px}.state-alert{margin-bottom:16px}.loading-state{padding:60px;text-align:center;color:var(--td-text-color-secondary)}
.log-list{display:flex;flex-direction:column;gap:10px}.log-card{background:var(--td-bg-color-container);border:1px solid var(--td-component-border);border-radius:10px;overflow:hidden}.log-main{display:grid;grid-template-columns:180px minmax(260px,1fr) 250px 70px;align-items:center;gap:16px;padding:15px 18px}.tool-cell,.subject,.metrics{display:flex;gap:9px;align-items:center}.tool-cell{flex-direction:column;align-items:flex-start}.time,.request-id,.metrics{font-size:12px;color:var(--td-text-color-secondary)}.subject{flex-direction:column;align-items:flex-start}.request-id{font-family:monospace}.detail-panel{border-top:1px solid var(--td-component-border);padding:18px;background:var(--td-bg-color-secondarycontainer)}.detail-panel h3{font-size:14px;margin:12px 0 7px}.detail-panel pre{white-space:pre-wrap;word-break:break-word;max-height:360px;overflow:auto;background:#0f172a;color:#dbeafe;padding:14px;border-radius:8px;font-size:12px}
@media(max-width:900px){.summary-grid{grid-template-columns:repeat(2,1fr)}.log-main{grid-template-columns:1fr}.filters{grid-template-columns:1fr}}
</style>
