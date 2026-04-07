<template>
  <div class="wiki-browser">
    <!-- Header -->
    <div class="wiki-header">
      <div class="wiki-header-left">
        <h2>Wiki Browser</h2>
        <span class="wiki-stats-badge" v-if="stats">
          {{ stats.total_pages }} pages &middot; {{ stats.total_links }} links
        </span>
      </div>
      <div class="wiki-header-actions">
        <input
          v-model="searchQuery"
          placeholder="Search wiki pages..."
          class="wiki-search-input"
          @keyup.enter="doSearch"
        />
        <select v-model="filterType" class="wiki-filter-select" @change="loadPages">
          <option value="">All Types</option>
          <option value="summary">Summaries</option>
          <option value="entity">Entities</option>
          <option value="concept">Concepts</option>
          <option value="synthesis">Synthesis</option>
          <option value="comparison">Comparisons</option>
        </select>
        <button class="wiki-btn wiki-btn-secondary" @click="activeTab = 'graph'">
          Graph View
        </button>
        <button class="wiki-btn wiki-btn-secondary" @click="runLint">
          Health Check
        </button>
      </div>
    </div>

    <!-- Tabs -->
    <div class="wiki-tabs">
      <button
        :class="['wiki-tab', { active: activeTab === 'pages' }]"
        @click="activeTab = 'pages'"
      >Pages</button>
      <button
        :class="['wiki-tab', { active: activeTab === 'graph' }]"
        @click="activeTab = 'graph'; loadGraph()"
      >Graph</button>
      <button
        :class="['wiki-tab', { active: activeTab === 'viewer' }]"
        @click="activeTab = 'viewer'"
        v-if="selectedPage"
      >{{ selectedPage.title }}</button>
    </div>

    <!-- Pages List -->
    <div v-if="activeTab === 'pages'" class="wiki-pages-list">
      <!-- Recent Activity -->
      <div v-if="logContent" class="wiki-recent-activity">
        <div class="wiki-recent-activity-header" @click="showLog = !showLog">
          <strong>Recent Activity</strong>
          <span class="wiki-toggle">{{ showLog ? '▼' : '▶' }}</span>
        </div>
        <div v-if="showLog" class="wiki-recent-activity-content" v-html="renderedLog"></div>
      </div>

      <div
        v-for="page in pages"
        :key="page.id"
        class="wiki-page-card"
        @click="selectPage(page)"
      >
        <div class="wiki-page-card-header">
          <span :class="['wiki-type-badge', `type-${page.page_type}`]">
            {{ page.page_type }}
          </span>
          <span class="wiki-page-title">{{ page.title }}</span>
          <span class="wiki-page-version">v{{ page.version }}</span>
        </div>
        <div class="wiki-page-summary">{{ page.summary }}</div>
        <div class="wiki-page-meta">
          <span>[[{{ page.slug }}]]</span>
          <span>{{ formatDate(page.updated_at) }}</span>
          <span v-if="page.out_links?.length">{{ page.out_links.length }} links</span>
        </div>
      </div>
      <div v-if="pages.length === 0" class="wiki-empty">
        No wiki pages yet. Upload documents to start building the wiki.
      </div>
    </div>

    <!-- Page Viewer -->
    <div v-if="activeTab === 'viewer' && selectedPage" class="wiki-page-viewer">
      <div class="wiki-viewer-header">
        <h3>{{ selectedPage.title }}</h3>
        <div class="wiki-viewer-meta">
          <span :class="['wiki-type-badge', `type-${selectedPage.page_type}`]">
            {{ selectedPage.page_type }}
          </span>
          <span>Version {{ selectedPage.version }}</span>
          <span>Updated {{ formatDate(selectedPage.updated_at) }}</span>
        </div>
      </div>
      <div class="wiki-viewer-links" v-if="selectedPage.in_links?.length || selectedPage.out_links?.length">
        <div v-if="selectedPage.out_links?.length">
          <strong>Links to:</strong>
          <a
            v-for="link in selectedPage.out_links"
            :key="link"
            href="#"
            class="wiki-link"
            @click.prevent="navigateToSlug(link)"
          >[[{{ link }}]]</a>
        </div>
        <div v-if="selectedPage.in_links?.length">
          <strong>Linked from:</strong>
          <a
            v-for="link in selectedPage.in_links"
            :key="link"
            href="#"
            class="wiki-link"
            @click.prevent="navigateToSlug(link)"
          >[[{{ link }}]]</a>
        </div>
      </div>
      <div class="wiki-viewer-content" v-html="renderedContent"></div>
    </div>

    <!-- Graph View -->
    <div v-if="activeTab === 'graph'" class="wiki-graph-container">
      <div ref="graphRef" class="wiki-graph-canvas"></div>
      <div v-if="!graphData" class="wiki-empty">Loading graph...</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import {
  listWikiPages,
  getWikiPage,
  getWikiGraph,
  getWikiStats,
  getWikiLog,
  searchWikiPages,
  type WikiPage,
  type WikiGraphData,
  type WikiStats,
} from '@/api/wiki'

const props = defineProps<{
  knowledgeBaseId: string
}>()

const pages = ref<WikiPage[]>([])
const selectedPage = ref<WikiPage | null>(null)
const stats = ref<WikiStats | null>(null)
const graphData = ref<WikiGraphData | null>(null)
const activeTab = ref<'pages' | 'viewer' | 'graph'>('pages')
const searchQuery = ref('')
const filterType = ref('')
const graphRef = ref<HTMLElement | null>(null)
const logContent = ref<string>('')
const showLog = ref(false)

// Render log content as simple HTML (last 5 entries)
const renderedLog = computed(() => {
  if (!logContent.value) return ''
  // Take last ~5 entries (split by ## headings)
  const entries = logContent.value.split(/(?=^## )/m).filter(e => e.trim())
  const recent = entries.slice(-5).reverse()
  let html = recent.join('\n')
  html = html.replace(/^## (.+)$/gm, '<h4>$1</h4>')
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/^- (.+)$/gm, '<li>$1</li>')
  html = html.replace(/\n/g, '<br>')
  return html
})

// Simple markdown-to-HTML rendering (replace wiki links with clickable ones)
const renderedContent = computed(() => {
  if (!selectedPage.value) return ''
  let html = selectedPage.value.content
  // Convert [[slug]] to clickable links
  html = html.replace(/\[\[([^\]]+)\]\]/g, (_, slug) => {
    return `<a href="#" class="wiki-link" data-slug="${slug}">[[${slug}]]</a>`
  })
  // Basic markdown headers
  html = html.replace(/^### (.+)$/gm, '<h3>$1</h3>')
  html = html.replace(/^## (.+)$/gm, '<h2>$1</h2>')
  html = html.replace(/^# (.+)$/gm, '<h1>$1</h1>')
  // Bold and italic
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>')
  // Line breaks
  html = html.replace(/\n\n/g, '</p><p>')
  html = html.replace(/\n/g, '<br>')
  return `<p>${html}</p>`
})

async function loadPages() {
  try {
    const params: any = { page: 1, page_size: 50 }
    if (filterType.value) params.page_type = filterType.value
    const res = await listWikiPages(props.knowledgeBaseId, params)
    pages.value = (res as any).data?.pages || (res as any).pages || []
  } catch (e) {
    console.error('Failed to load wiki pages:', e)
  }
}

async function loadStats() {
  try {
    const res = await getWikiStats(props.knowledgeBaseId)
    stats.value = (res as any).data || res as any
  } catch (e) {
    console.error('Failed to load wiki stats:', e)
  }
}

async function loadLog() {
  try {
    const res = await getWikiLog(props.knowledgeBaseId)
    const page = (res as any).data || res as any
    logContent.value = page?.content || ''
  } catch (e) {
    // Log page may not exist yet
    logContent.value = ''
  }
}

async function loadGraph() {
  try {
    const res = await getWikiGraph(props.knowledgeBaseId)
    graphData.value = (res as any).data || res as any
  } catch (e) {
    console.error('Failed to load wiki graph:', e)
  }
}

async function selectPage(page: WikiPage) {
  try {
    const res = await getWikiPage(props.knowledgeBaseId, page.slug)
    selectedPage.value = (res as any).data || res as any
    activeTab.value = 'viewer'
  } catch (e) {
    console.error('Failed to load wiki page:', e)
  }
}

async function navigateToSlug(slug: string) {
  try {
    const res = await getWikiPage(props.knowledgeBaseId, slug)
    selectedPage.value = (res as any).data || res as any
    activeTab.value = 'viewer'
  } catch (e) {
    console.error(`Failed to navigate to ${slug}:`, e)
  }
}

async function doSearch() {
  if (!searchQuery.value.trim()) {
    loadPages()
    return
  }
  try {
    const res = await searchWikiPages(props.knowledgeBaseId, searchQuery.value)
    pages.value = (res as any).data?.pages || (res as any).pages || []
  } catch (e) {
    console.error('Wiki search failed:', e)
  }
}

function runLint() {
  // Open stats/lint view
  loadStats()
  alert(`Wiki Health:\nTotal pages: ${stats.value?.total_pages || 0}\nOrphan pages: ${stats.value?.orphan_count || 0}\nTotal links: ${stats.value?.total_links || 0}`)
}

function formatDate(dateStr: string) {
  if (!dateStr) return ''
  return new Date(dateStr).toLocaleDateString()
}

onMounted(() => {
  loadPages()
  loadStats()
  loadLog()
})
</script>

<style scoped>
.wiki-browser {
  padding: 16px;
}
.wiki-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.wiki-header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}
.wiki-header-left h2 {
  margin: 0;
  font-size: 20px;
}
.wiki-stats-badge {
  background: #f0f0f0;
  padding: 4px 8px;
  border-radius: 12px;
  font-size: 12px;
  color: #666;
}
.wiki-header-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}
.wiki-search-input {
  padding: 6px 12px;
  border: 1px solid #ddd;
  border-radius: 6px;
  width: 200px;
}
.wiki-filter-select {
  padding: 6px 8px;
  border: 1px solid #ddd;
  border-radius: 6px;
}
.wiki-btn {
  padding: 6px 12px;
  border: 1px solid #ddd;
  border-radius: 6px;
  cursor: pointer;
  background: white;
}
.wiki-btn:hover {
  background: #f5f5f5;
}
.wiki-btn-secondary {
  color: #666;
}
.wiki-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid #eee;
  margin-bottom: 16px;
}
.wiki-tab {
  padding: 8px 16px;
  border: none;
  background: none;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  color: #666;
}
.wiki-tab.active {
  color: #1a73e8;
  border-bottom-color: #1a73e8;
}
.wiki-page-card {
  padding: 12px 16px;
  border: 1px solid #eee;
  border-radius: 8px;
  margin-bottom: 8px;
  cursor: pointer;
  transition: border-color 0.2s;
}
.wiki-page-card:hover {
  border-color: #1a73e8;
}
.wiki-page-card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}
.wiki-type-badge {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 500;
}
.type-summary { background: #e3f2fd; color: #1565c0; }
.type-entity { background: #e8f5e9; color: #2e7d32; }
.type-concept { background: #fff3e0; color: #e65100; }
.type-synthesis { background: #f3e5f5; color: #6a1b9a; }
.type-comparison { background: #fce4ec; color: #c62828; }
.type-index { background: #f5f5f5; color: #616161; }
.type-log { background: #f5f5f5; color: #616161; }
.wiki-page-title {
  font-weight: 600;
  flex: 1;
}
.wiki-page-version {
  font-size: 12px;
  color: #999;
}
.wiki-page-summary {
  color: #666;
  font-size: 13px;
  margin-bottom: 4px;
}
.wiki-page-meta {
  display: flex;
  gap: 16px;
  font-size: 12px;
  color: #999;
}
.wiki-empty {
  text-align: center;
  padding: 40px;
  color: #999;
}
.wiki-recent-activity {
  margin-bottom: 16px;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
  overflow: hidden;
}
.wiki-recent-activity-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  background: #fafafa;
  cursor: pointer;
  font-size: 14px;
}
.wiki-recent-activity-header:hover {
  background: #f0f0f0;
}
.wiki-toggle {
  font-size: 10px;
  color: #999;
}
.wiki-recent-activity-content {
  padding: 12px 16px;
  font-size: 13px;
  color: #555;
  max-height: 300px;
  overflow-y: auto;
  line-height: 1.6;
}
.wiki-recent-activity-content :deep(h4) {
  font-size: 14px;
  margin: 8px 0 4px;
  color: #333;
}
.wiki-recent-activity-content :deep(li) {
  margin-left: 16px;
  list-style: disc;
}
.wiki-page-viewer {
  max-width: 800px;
}
.wiki-viewer-header {
  margin-bottom: 16px;
}
.wiki-viewer-header h3 {
  margin: 0 0 8px;
}
.wiki-viewer-meta {
  display: flex;
  gap: 12px;
  font-size: 13px;
  color: #666;
  align-items: center;
}
.wiki-viewer-links {
  padding: 12px;
  background: #f8f9fa;
  border-radius: 6px;
  margin-bottom: 16px;
  font-size: 13px;
}
.wiki-viewer-links > div {
  margin-bottom: 4px;
}
.wiki-link {
  color: #1a73e8;
  text-decoration: none;
  margin-left: 8px;
}
.wiki-link:hover {
  text-decoration: underline;
}
.wiki-viewer-content {
  line-height: 1.7;
}
.wiki-viewer-content :deep(h1) { font-size: 24px; margin-top: 24px; }
.wiki-viewer-content :deep(h2) { font-size: 20px; margin-top: 20px; }
.wiki-viewer-content :deep(h3) { font-size: 16px; margin-top: 16px; }
.wiki-graph-container {
  height: 500px;
  border: 1px solid #eee;
  border-radius: 8px;
  position: relative;
}
.wiki-graph-canvas {
  width: 100%;
  height: 100%;
}
</style>
