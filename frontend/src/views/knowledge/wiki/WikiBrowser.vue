<template>
  <div class="wiki-browser">
    <!-- Graph view (full screen) -->
    <template v-if="view === 'graph'">
      <div class="wiki-graph">
        <div ref="graphRef" class="wiki-graph-canvas"></div>

        <!-- Graph Search Overlay -->
        <div v-if="graphReady" class="wiki-graph-search">
          <t-select
            v-model="graphSearchValue"
            filterable
            :options="graphSearchOptions"
            :placeholder="$t('knowledgeEditor.wikiBrowser.searchPlaceholder')"
            @change="handleGraphSearchSelect"
            @enter="handleGraphSearchEnter"
            :popup-props="{ zIndex: 100 }"
            class="graph-search-select"
          >
            <template #prefixIcon><t-icon name="search" /></template>
          </t-select>
        </div>

        <!-- Legend Overlay -->
        <div v-if="graphReady" class="wiki-graph-legend">
          <div class="legend-items">
            <div class="legend-item">
              <span class="legend-dot" style="background: #0052d9"></span>
              {{ $t('knowledgeEditor.wikiBrowser.filterSummary') }}
            </div>
            <div class="legend-item">
              <span class="legend-dot" style="background: #2ba471"></span>
              {{ $t('knowledgeEditor.wikiBrowser.filterEntity') }}
            </div>
            <div class="legend-item">
              <span class="legend-dot" style="background: #e37318"></span>
              {{ $t('knowledgeEditor.wikiBrowser.filterConcept') }}
            </div>
            <div class="legend-item">
              <span class="legend-dot" style="background: #0594fa"></span>
              {{ $t('knowledgeEditor.wikiBrowser.filterSynthesis') }}
            </div>
            <div class="legend-item">
              <span class="legend-dot" style="background: #d54941"></span>
              {{ $t('knowledgeEditor.wikiBrowser.filterComparison') }}
            </div>
          </div>
          <div class="legend-divider"></div>
          <div class="legend-action" @click="toggleArrows" :class="{ active: showArrows }">
            <span class="legend-action-icon">→</span>
            <span>{{ showArrows ? $t('knowledgeEditor.wikiBrowser.hideArrows') : $t('knowledgeEditor.wikiBrowser.showArrows') }}</span>
          </div>
        </div>

        <div v-if="!graphReady" class="wiki-reader-empty">
          <t-loading v-if="graphLoading" />
          <div v-else class="wiki-empty-icon">
            <t-icon name="chart-ring" size="48px" />
          </div>
          <p class="wiki-empty-desc">{{ graphLoading ? $t('knowledgeEditor.wikiBrowser.graphEmpty') : $t('knowledgeEditor.wikiBrowser.graphNoData') }}</p>
        </div>

        <!-- Graph page detail drawer -->
        <t-drawer
          v-model:visible="graphDrawerVisible"
          :header="graphDrawerPage?.title || ''"
          size="480px"
          :footer="false"
          placement="right"
          :attach="false"
          :show-overlay="false"
          :close-btn="true"
          destroy-on-close
          class="wiki-graph-drawer"
        >
          <template v-if="graphDrawerPage">
            <div class="wiki-reader-meta" style="margin-bottom: 16px;">
              <t-tag size="small" :theme="getTypeTheme(graphDrawerPage.page_type)" variant="light-outline">
                {{ getTypeLabel(graphDrawerPage.page_type) }}
              </t-tag>
              <span class="wiki-reader-meta-text">{{ $t('knowledgeEditor.wikiBrowser.version', { ver: graphDrawerPage.version }) }}</span>
            </div>
            <div ref="drawerBodyRef" class="wiki-reader-body" v-html="graphDrawerContent" @click="handleGraphDrawerClick"></div>
          </template>
        </t-drawer>
      </div>
    </template>

    <!-- Browser view (left list + right reader) -->
    <template v-else>
      <!-- Left Panel: Page List -->
      <aside class="wiki-sidebar">
        <div class="wiki-sidebar-header">
          <t-input
            v-model="searchQuery"
            :placeholder="$t('knowledgeEditor.wikiBrowser.searchPlaceholder')"
            clearable
            @enter="doSearch"
            @clear="loadPages"
          >
            <template #prefixIcon><t-icon name="search" /></template>
          </t-input>
        </div>

        <div class="wiki-page-list">
          <!-- Index page (pinned at top) -->
          <div
            v-if="indexPage"
            :class="['wiki-nav-item', { active: selectedPage?.id === indexPage.id }]"
            @click="selectPage(indexPage)"
          >
            <t-icon name="catalog" class="wiki-nav-icon" />
            <span class="wiki-nav-text">{{ $t('knowledgeEditor.wikiBrowser.indexTitle') }}</span>
          </div>

          <!-- Log page (pinned) -->
          <div
            v-if="logPage"
            :class="['wiki-nav-item', { active: selectedPage?.id === logPage.id }]"
            @click="selectPage(logPage)"
          >
            <t-icon name="history" class="wiki-nav-icon" />
            <span class="wiki-nav-text">{{ $t('knowledgeEditor.wikiBrowser.logTitle') }}</span>
          </div>

          <div class="wiki-sidebar-divider" v-if="indexPage || logPage"></div>

          <!-- Grouped by type (collapsible) -->
          <template v-for="group in groupedPages" :key="group.type">
            <div
              class="wiki-group-label"
              @click="toggleGroup(group.type)"
            >
              <t-icon
                :name="collapsedGroups[group.type] ? 'chevron-right' : 'chevron-down'"
                size="12px"
                class="wiki-group-chevron"
              />
              {{ group.label }}
              <span class="wiki-group-count">{{ group.pages.length }}</span>
            </div>
            <template v-if="!collapsedGroups[group.type]">
              <div
                v-for="page in group.pages"
                :key="page.id"
                :class="['wiki-page-item', { active: selectedPage?.id === page.id }]"
                @click="selectPage(page)"
              >
                <div class="wiki-page-item-title">{{ page.title }}</div>
                <div class="wiki-page-item-summary">{{ page.summary }}</div>
                <div class="wiki-page-item-meta">
                  <span>{{ formatDate(page.updated_at) }}</span>
                </div>
              </div>
            </template>
          </template>

          <!-- Empty state -->
          <div v-if="contentPages.length === 0 && !loading" class="wiki-empty-state">
            <div class="wiki-empty-icon">
              <t-icon name="file-unknown" size="36px" />
            </div>
            <p class="wiki-empty-title">{{ $t('knowledgeEditor.wikiBrowser.emptyTitle') }}</p>
            <p class="wiki-empty-desc">{{ $t('knowledgeEditor.wikiBrowser.emptyDesc') }}</p>
          </div>
        </div>
      </aside>

      <!-- Right Panel: Reader -->
      <div class="wiki-content">
        <div class="wiki-reader">
          <div class="wiki-reader-inner">
            <template v-if="selectedPage">
              <!-- Navigation -->
              <div v-if="navHistory.length" class="wiki-nav-bar">
                <a href="#" class="wiki-nav-back" @click.prevent="goBack">
                  <t-icon name="arrow-left" size="14px" />
                  <span>{{ navHistory[navHistory.length - 1].title }}</span>
                </a>
              </div>

              <!-- Page header -->
              <div class="wiki-reader-header">
                <h2 class="wiki-reader-title">{{ selectedPage.title }}</h2>
                <div class="wiki-reader-meta">
                  <t-tag size="small" :theme="getTypeTheme(selectedPage.page_type)" variant="light-outline">
                    {{ getTypeLabel(selectedPage.page_type) }}
                  </t-tag>
                  <span class="wiki-reader-meta-text">{{ $t('knowledgeEditor.wikiBrowser.version', { ver: selectedPage.version }) }}</span>
                  <span class="wiki-reader-meta-text">{{ formatDate(selectedPage.updated_at) }}</span>
                </div>
              </div>

              <!-- Backlinks (in_links) -->
              <div v-if="selectedPage.in_links?.length" class="wiki-reader-backlinks">
                <span class="wiki-backlink-label">
                  <t-icon name="link" size="14px" />
                  {{ $t('knowledgeEditor.wikiBrowser.linkedFrom') }}
                </span>
                <a
                  v-for="link in selectedPage.in_links"
                  :key="'in-' + link"
                  href="#"
                  class="wiki-backlink-tag"
                  @click.prevent="navigateToSlug(link)"
                >{{ slugDisplayName(link) }}</a>
              </div>

              <!-- Content -->
              <div ref="readerBodyRef" class="wiki-reader-body" v-html="renderedContent" @click="handleContentClick"></div>

              <!-- Source refs -->
              <div v-if="parsedSourceRefs.length" class="wiki-reader-sources">
                <span class="wiki-link-label">{{ $t('knowledgeEditor.wikiBrowser.sources') }}</span>
                <a
                  v-for="ref in parsedSourceRefs"
                  :key="ref.id"
                  href="#"
                  class="wiki-source-ref"
                  @click.prevent="emit('open-source-doc', ref.id)"
                >
                  <t-icon name="file" size="14px" />
                  {{ ref.title }}
                </a>
              </div>
            </template>

            <!-- No page selected -->
            <div v-else class="wiki-reader-empty">
              <div class="wiki-empty-icon">
                <t-icon name="browse" size="48px" />
              </div>
              <p class="wiki-empty-title" v-if="contentPages.length > 0">{{ $t('knowledgeEditor.wikiBrowser.selectPageHint') }}</p>
              <template v-else>
                <p class="wiki-empty-title">{{ $t('knowledgeEditor.wikiBrowser.emptyTitle') }}</p>
                <p class="wiki-empty-desc">{{ $t('knowledgeEditor.wikiBrowser.emptyDesc') }}</p>
              </template>
            </div>
          </div>
        </div>
      </div>
    </template>
    
    <!-- Image Preview -->
    <Teleport to="body">
      <picturePreview v-if="imagePreviewVisible" :reviewImg="imagePreviewVisible" :reviewUrl="imagePreviewUrl" @closePreImg="closeImagePreview" />
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import { hydrateProtectedFileImages } from '@/utils/security'
import picturePreview from '@/components/picture-preview.vue'
import {
  listWikiPages,
  getWikiPage,
  getWikiGraph,
  getWikiStats,
  searchWikiPages,
  type WikiPage,
  type WikiGraphData,
  type WikiStats,
} from '@/api/wiki'

const { t } = useI18n()

const props = defineProps<{
  knowledgeBaseId: string
  view?: 'browser' | 'graph'
}>()

const emit = defineEmits<{
  (e: 'open-source-doc', knowledgeId: string): void
}>()
const pages = ref<WikiPage[]>([])
const selectedPage = ref<WikiPage | null>(null)
const stats = ref<WikiStats | null>(null)
const graphData = ref<WikiGraphData | null>(null)
const searchQuery = ref('')
const graphSearchValue = ref('')
const graphRef = ref<HTMLElement | null>(null)
const readerBodyRef = ref<HTMLElement | null>(null)
const drawerBodyRef = ref<HTMLElement | null>(null)
const loading = ref(false)
const graphLoading = ref(false)
const graphReady = ref(false)
const showArrows = ref(true)
const collapsedGroups = reactive<Record<string, boolean>>({})
const graphDrawerVisible = ref(false)
const graphDrawerPage = ref<WikiPage | null>(null)
const navHistory = ref<WikiPage[]>([])
// Index and log pages (pinned at top)
const indexPage = computed(() => pages.value.find(p => p.page_type === 'index'))
const logPage = computed(() => pages.value.find(p => p.page_type === 'log'))

// Filter out system pages (index, log) for the grouped list
const contentPages = computed(() =>
  pages.value.filter(p => p.page_type !== 'index' && p.page_type !== 'log')
)

// Group pages by type for display
const typeOrder = ['summary', 'entity', 'concept', 'synthesis', 'comparison']

const groupedPages = computed(() => {
  const groups: { type: string; label: string; pages: WikiPage[] }[] = []
  const byType = new Map<string, WikiPage[]>()

  for (const page of contentPages.value) {
    const arr = byType.get(page.page_type) || []
    arr.push(page)
    byType.set(page.page_type, arr)
  }

  for (const type of typeOrder) {
    const pages = byType.get(type)
    if (pages && pages.length > 0) {
      groups.push({ type, label: getTypeLabel(type), pages })
    }
  }

  // Any remaining types not in typeOrder
  for (const [type, pages] of byType) {
    if (!typeOrder.includes(type) && pages.length > 0) {
      groups.push({ type, label: getTypeLabel(type), pages })
    }
  }

  return groups
})

// Parse source refs in "id|title" format
const parsedSourceRefs = computed(() => {
  if (!selectedPage.value?.source_refs?.length) return []
  return selectedPage.value.source_refs.map(ref => {
    const pipeIdx = ref.indexOf('|')
    if (pipeIdx > 0) {
      return { id: ref.substring(0, pipeIdx), title: ref.substring(pipeIdx + 1) }
    }
    // Fallback: show raw ref (backwards compat with old data)
    return { id: ref, title: ref.length > 20 ? ref.substring(0, 8) + '...' : ref }
  })
})

// Rendered content for graph drawer
const graphDrawerContent = computed(() => {
  if (!graphDrawerPage.value) return ''
  return renderMarkdown(graphDrawerPage.value.content)
})

const imagePreviewVisible = ref(false)
const imagePreviewUrl = ref('')

function closeImagePreview() {
  imagePreviewVisible.value = false
  imagePreviewUrl.value = ''
}

watch(graphDrawerContent, async () => {
  await nextTick()
  if (drawerBodyRef.value) {
    await hydrateProtectedFileImages(drawerBodyRef.value)
  }
})

function renderMarkdown(content: string): string {
  // Pre-process wiki links [[slug|name]] to custom HTML tags
  let preprocessed = content.replace(/\[\[([^\]]+)\]\]/g, (_, inner: string) => {
    const pipeIdx = inner.indexOf('|')
    const slug = pipeIdx > 0 ? inner.substring(0, pipeIdx).trim() : inner.trim()
    const display = pipeIdx > 0 ? inner.substring(pipeIdx + 1).trim() : slugDisplayName(slug)
    return `<a href="#" class="wiki-content-link" data-slug="${slug}">${display}</a>`
  })

  // Use marked to render the markdown to HTML
  return marked.parse(preprocessed, { breaks: true, async: false }) as string
}

async function openGraphDrawer(slug: string) {
  try {
    const res = await getWikiPage(props.knowledgeBaseId, slug)
    graphDrawerPage.value = (res as any).data || res as any
    graphDrawerVisible.value = true
  } catch (e) {
    console.error(`Failed to load page ${slug}:`, e)
  }
}

function handleGraphDrawerClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (target.classList.contains('wiki-content-link')) {
    e.preventDefault()
    const slug = target.getAttribute('data-slug')
    if (slug) handleGraphSearchSelect(slug)
  } else if (target.tagName.toLowerCase() === 'img') {
    e.preventDefault()
    imagePreviewUrl.value = target.getAttribute('src') || ''
    if (imagePreviewUrl.value) {
      imagePreviewVisible.value = true
    }
  }
}

function toggleGroup(type: string) {
  collapsedGroups[type] = !collapsedGroups[type]
}

function getTypeTheme(type: string): string {
  const map: Record<string, string> = {
    summary: 'primary', entity: 'success', concept: 'warning',
    synthesis: 'primary', comparison: 'danger', index: 'default', log: 'default',
  }
  return map[type] || 'default'
}

function getTypeLabel(type: string): string {
  const map: Record<string, string> = {
    summary: t('knowledgeEditor.wikiBrowser.filterSummary'),
    entity: t('knowledgeEditor.wikiBrowser.filterEntity'),
    concept: t('knowledgeEditor.wikiBrowser.filterConcept'),
    synthesis: t('knowledgeEditor.wikiBrowser.filterSynthesis'),
    comparison: t('knowledgeEditor.wikiBrowser.filterComparison'),
    index: 'Index',
    log: 'Log',
  }
  return map[type] || type
}

const renderedContent = computed(() => {
  if (!selectedPage.value) return ''
  return renderMarkdown(selectedPage.value.content)
})

watch(renderedContent, async () => {
  await nextTick()
  if (readerBodyRef.value) {
    await hydrateProtectedFileImages(readerBodyRef.value)
  }
})

function handleContentClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (target.classList.contains('wiki-content-link')) {
    e.preventDefault()
    const slug = target.getAttribute('data-slug')
    if (slug) navigateToSlug(slug)
  } else if (target.tagName.toLowerCase() === 'img') {
    e.preventDefault()
    imagePreviewUrl.value = target.getAttribute('src') || ''
    if (imagePreviewUrl.value) {
      imagePreviewVisible.value = true
    }
  }
}

async function loadPages() {
  loading.value = true
  try {
    const res = await listWikiPages(props.knowledgeBaseId, { page: 1, page_size: 500 })
    pages.value = (res as any).data?.pages || (res as any).pages || []
    // Auto-select index page if nothing is selected
    if (!selectedPage.value && indexPage.value) {
      selectPage(indexPage.value)
    }
  } catch (e) {
    console.error('Failed to load wiki pages:', e)
  } finally {
    loading.value = false
  }
}

async function loadStats() {
  try {
    const res = await getWikiStats(props.knowledgeBaseId)
    stats.value = (res as any).data || res as any
  } catch (e) { /* ignore */ }
}

async function loadGraph() {
  graphLoading.value = true
  graphReady.value = false
  try {
    const res = await getWikiGraph(props.knowledgeBaseId)
    graphData.value = (res as any).data || res as any
    await nextTick()
    renderGraph()
  } catch (e) {
    console.error('Failed to load graph:', e)
  } finally {
    graphLoading.value = false
  }
}

async function selectPage(page: WikiPage) {
  try {
    if (selectedPage.value && selectedPage.value.id !== page.id) {
      navHistory.value.push(selectedPage.value)
    }
    const res = await getWikiPage(props.knowledgeBaseId, page.slug)
    selectedPage.value = (res as any).data || res as any
  } catch (e) {
    console.error('Failed to load wiki page:', e)
  }
}

async function navigateToSlug(slug: string) {
  try {
    if (selectedPage.value && selectedPage.value.slug !== slug) {
      navHistory.value.push(selectedPage.value)
    }
    const res = await getWikiPage(props.knowledgeBaseId, slug)
    selectedPage.value = (res as any).data || res as any
  } catch (e) {
    console.error(`Failed to navigate to ${slug}:`, e)
  }
}

function goBack() {
  const prev = navHistory.value.pop()
  if (prev) {
    selectedPage.value = prev
  }
}

async function doSearch() {
  if (!searchQuery.value.trim()) { loadPages(); return }
  loading.value = true
  try {
    const res = await searchWikiPages(props.knowledgeBaseId, searchQuery.value)
    pages.value = (res as any).data?.pages || (res as any).pages || []
  } catch (e) { console.error('Wiki search failed:', e) }
  finally { loading.value = false }
}

function toggleArrows() {
  showArrows.value = !showArrows.value
  for (const e of graphEdgeElsRef) {
    if (showArrows.value) {
      e.line.setAttribute('marker-end', 'url(#arrow-end)')
      if (e.bidir) e.line.setAttribute('marker-start', 'url(#arrow-start)')
    } else {
      e.line.removeAttribute('marker-end')
      e.line.removeAttribute('marker-start')
    }
  }
}

function formatDate(dateStr: string) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// Convert slug like "entity/acme-corp" to a readable label "acme-corp"
function slugDisplayName(slug: string): string {
  // Find the page title if loaded
  const page = pages.value.find(p => p.slug === slug)
  if (page) return page.title
  // Fallback: strip type prefix, replace hyphens
  const parts = slug.split('/')
  return parts.length > 1 ? parts.slice(1).join('/') : slug
}

// ─── Graph Rendering (interactive SVG force-directed graph) ───
// Features: drag nodes, pan canvas, zoom, hover highlight, click to open drawer, legend

interface GNode {
  x: number; y: number; vx: number; vy: number
  slug: string; title: string; type: string
  linkCount: number; pinned: boolean
}

// Persistent graph state so it survives re-renders
let graphNodes: GNode[] = []
let graphSvg: SVGSVGElement | null = null
let graphAnimFrame = 0

// Used for graph search centering interaction
let graphPanZoomRef: {
  setScale: (s: number) => void,
  setTranslate: (x: number, y: number) => void,
  apply: () => void,
  flyTo: (x: number, y: number, s?: number, duration?: number) => void,
  getScale: () => number
} | null = null

const graphHighlightSlug = ref<string | null>(null)
const graphSelectedSlug = ref<string | null>(null)

// Color map for node types
const nodeColorMap: Record<string, string> = {
  summary: '#0052d9', entity: '#2ba471', concept: '#e37318',
  synthesis: '#0594fa', comparison: '#d54941', index: '#8c8c8c', log: '#8c8c8c',
}

function renderGraph() {
  const container = graphRef.value
  const data = graphData.value
  if (!container || !data || !data.nodes?.length) return

  // Stop any previous animation
  if (graphAnimFrame) { cancelAnimationFrame(graphAnimFrame); graphAnimFrame = 0 }

  const width = container.clientWidth || 800
  const height = container.clientHeight || 600

  // Create SVG
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('viewBox', `0 0 ${width} ${height}`)
  svg.style.width = '100%'
  svg.style.height = '100%'
  container.innerHTML = ''
  container.appendChild(svg)
  graphSvg = svg

  // Root group for pan/zoom transform
  const rootG = document.createElementNS('http://www.w3.org/2000/svg', 'g')
  rootG.setAttribute('class', 'graph-root')
  svg.appendChild(rootG)

  // Edge group (below nodes)
  const edgeG = document.createElementNS('http://www.w3.org/2000/svg', 'g')
  rootG.appendChild(edgeG)

  // Node group (above edges)
  const nodeG = document.createElementNS('http://www.w3.org/2000/svg', 'g')
  rootG.appendChild(nodeG)

  // Build adjacency for highlight
  const adjacency = new Map<string, Set<string>>()
  for (const edge of data.edges) {
    if (!adjacency.has(edge.source)) adjacency.set(edge.source, new Set())
    if (!adjacency.has(edge.target)) adjacency.set(edge.target, new Set())
    adjacency.get(edge.source)!.add(edge.target)
    adjacency.get(edge.target)!.add(edge.source)
  }

  // Build nodes
  const nodeMap = new Map<string, GNode>()
  graphNodes = data.nodes.map((n, i) => {
    const angle = (2 * Math.PI * i) / data.nodes.length
    const r = Math.min(width, height) * 0.35
    const node: GNode = {
      x: width / 2 + r * Math.cos(angle) + (Math.random() - 0.5) * 50,
      y: height / 2 + r * Math.sin(angle) + (Math.random() - 0.5) * 50,
      vx: 0, vy: 0,
      slug: n.slug, title: n.title, type: n.page_type,
      linkCount: n.link_count || 0, pinned: false,
    }
    nodeMap.set(n.slug, node)
    return node
  })

  // Node radius based on link count
  function nodeRadius(n: GNode) { return Math.max(6, Math.min(18, 6 + n.linkCount * 1.5)) }

  // Define arrow markers in SVG <defs>
  const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs')

  // Single-direction arrow (at end)
  const markerEnd = document.createElementNS('http://www.w3.org/2000/svg', 'marker')
  markerEnd.setAttribute('id', 'arrow-end')
  markerEnd.setAttribute('viewBox', '0 0 10 6')
  markerEnd.setAttribute('refX', '10')
  markerEnd.setAttribute('refY', '3')
  markerEnd.setAttribute('markerWidth', '8')
  markerEnd.setAttribute('markerHeight', '6')
  markerEnd.setAttribute('orient', 'auto')
  const arrowPath = document.createElementNS('http://www.w3.org/2000/svg', 'path')
  arrowPath.setAttribute('d', 'M0,0 L10,3 L0,6 L2,3 Z')
  arrowPath.setAttribute('fill', '#c0c4cc')
  markerEnd.appendChild(arrowPath)
  defs.appendChild(markerEnd)

  // Bidirectional: arrow at start (reverse)
  const markerStart = document.createElementNS('http://www.w3.org/2000/svg', 'marker')
  markerStart.setAttribute('id', 'arrow-start')
  markerStart.setAttribute('viewBox', '0 0 10 6')
  markerStart.setAttribute('refX', '0')
  markerStart.setAttribute('refY', '3')
  markerStart.setAttribute('markerWidth', '8')
  markerStart.setAttribute('markerHeight', '6')
  markerStart.setAttribute('orient', 'auto')
  const arrowPathStart = document.createElementNS('http://www.w3.org/2000/svg', 'path')
  arrowPathStart.setAttribute('d', 'M10,0 L0,3 L10,6 L8,3 Z')
  arrowPathStart.setAttribute('fill', '#c0c4cc')
  markerStart.appendChild(arrowPathStart)
  defs.appendChild(markerStart)

  // Highlighted arrows
  for (const id of ['arrow-end-hl', 'arrow-start-hl']) {
    const m = document.createElementNS('http://www.w3.org/2000/svg', 'marker')
    m.setAttribute('id', id)
    m.setAttribute('viewBox', '0 0 10 6')
    m.setAttribute('refX', id.includes('end') ? '10' : '0')
    m.setAttribute('refY', '3')
    m.setAttribute('markerWidth', '8')
    m.setAttribute('markerHeight', '6')
    m.setAttribute('orient', 'auto')
    const p = document.createElementNS('http://www.w3.org/2000/svg', 'path')
    p.setAttribute('d', id.includes('end') ? 'M0,0 L10,3 L0,6 L2,3 Z' : 'M10,0 L0,3 L10,6 L8,3 Z')
    p.setAttribute('fill', '#0052d9')
    m.appendChild(p)
    defs.appendChild(m)
  }

  // Drop shadow filter for nodes
  const filter = document.createElementNS('http://www.w3.org/2000/svg', 'filter')
  filter.setAttribute('id', 'node-shadow')
  filter.setAttribute('x', '-20%')
  filter.setAttribute('y', '-20%')
  filter.setAttribute('width', '140%')
  filter.setAttribute('height', '140%')
  filter.innerHTML = `<feDropShadow dx="0" dy="2" stdDeviation="3" flood-color="#000" flood-opacity="0.15"/>`
  defs.appendChild(filter)

  svg.appendChild(defs)

  // Detect bidirectional edges (A→B and B→A both exist)
  const edgePairSet = new Set<string>()
  for (const edge of data.edges) {
    edgePairSet.add(`${edge.source}→${edge.target}`)
  }

  // Create SVG elements for edges (deduplicate bidirectional into single line with double arrows)
  type EdgeEl = { line: SVGLineElement; source: string; target: string; bidir: boolean }
  const edgeEls: EdgeEl[] = []
  const processedPairs = new Set<string>()

  for (const edge of data.edges) {
    const pairKey = [edge.source, edge.target].sort().join('↔')
    if (processedPairs.has(pairKey)) continue
    processedPairs.add(pairKey)

    const bidir = edgePairSet.has(`${edge.target}→${edge.source}`)

    const line = document.createElementNS('http://www.w3.org/2000/svg', 'line')
    line.setAttribute('stroke', '#c0c4cc')
    line.setAttribute('stroke-width', '1.2')
    line.setAttribute('stroke-opacity', '0.4')
    line.setAttribute('marker-end', 'url(#arrow-end)')
    line.style.transition = 'stroke 0.2s, stroke-width 0.2s, stroke-opacity 0.2s'
    if (bidir) {
      line.setAttribute('marker-start', 'url(#arrow-start)')
    }
    edgeG.appendChild(line)
    edgeEls.push({ line, source: edge.source, target: edge.target, bidir })
  }

  // Create SVG elements for nodes
  const nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; activeRing: SVGCircleElement; node: GNode }[] = []
  for (const n of graphNodes) {
    const g = document.createElementNS('http://www.w3.org/2000/svg', 'g')
    g.style.cursor = 'pointer'

    const r = nodeRadius(n)

    // Pulse ring for selected state
    const activeRing = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
    activeRing.setAttribute('r', String(r + 5))
    activeRing.setAttribute('fill', 'none')
    activeRing.setAttribute('stroke', nodeColorMap[n.type] || '#8c8c8c')
    activeRing.setAttribute('stroke-width', '2')
    activeRing.style.opacity = '0'
    activeRing.style.transition = 'opacity 0.2s'
    activeRing.classList.add('node-active-ring')
    g.appendChild(activeRing)

    const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
    circle.setAttribute('r', String(r))
    circle.setAttribute('fill', nodeColorMap[n.type] || '#8c8c8c')
    circle.setAttribute('stroke', '#fff')
    circle.setAttribute('stroke-width', '2')
    circle.setAttribute('filter', 'url(#node-shadow)')
    circle.style.transition = 'r 0.2s, stroke-width 0.2s, opacity 0.2s'
    g.appendChild(circle)

    // Text label wrapper for better readability
    const textBg = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
    g.appendChild(textBg) // we'll size this after we know text size

    const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
    text.setAttribute('text-anchor', 'middle')
    text.setAttribute('dy', String(r + 14))
    text.setAttribute('font-size', '11')
    text.setAttribute('fill', 'var(--td-text-color-secondary)')
    text.setAttribute('pointer-events', 'none')
    text.style.textShadow = '0 1px 3px var(--td-bg-color-container), 0 -1px 3px var(--td-bg-color-container), 1px 0 3px var(--td-bg-color-container), -1px 0 3px var(--td-bg-color-container)'
    text.textContent = n.title.length > 14 ? n.title.substring(0, 14) + '…' : n.title
    g.appendChild(text)

    // Hover highlight
    g.addEventListener('mouseenter', () => {
      // Don't change highlight on hover if we have a selected node
      if (!graphSelectedSlug.value) {
        graphHighlightSlug.value = n.slug
        applyHighlight(n.slug, adjacency, nodeEls, edgeEls)
      } else if (graphSelectedSlug.value !== n.slug) {
        // If a node is selected, hover over others should just add to highlight
        // but we'll keep the selected one as the main focus
        applyHighlight(graphSelectedSlug.value, adjacency, nodeEls, edgeEls, n.slug)
      }
    })
    g.addEventListener('mouseleave', () => {
      // Only clear if we were just highlighting on hover, not selected
      if (!graphSelectedSlug.value) {
        graphHighlightSlug.value = null
        clearHighlight(nodeEls, edgeEls)
      } else {
        // Restore pure selected state
        applyHighlight(graphSelectedSlug.value, adjacency, nodeEls, edgeEls)
      }
    })

    // Click to select & open drawer directly
    g.addEventListener('click', (e) => {
      e.stopPropagation()
      
      // Select and highlight
      graphSelectedSlug.value = n.slug
      applyHighlight(n.slug, adjacency, nodeEls, edgeEls)
      
      // Auto pan to center the node, shifted left for drawer
      if (graphPanZoomRef) {
        const container = graphRef.value
        if (container) {
          const width = container.clientWidth
          const height = container.clientHeight
          graphPanZoomRef.flyTo(
            width / 2 - n.x * graphPanZoomRef.getScale() - 240,
            height / 2 - n.y * graphPanZoomRef.getScale()
          )
        }
      }
      
      // Open drawer (it will handle drawer visibility and fetching content)
      openGraphDrawer(n.slug)
    })

    // Drag support
    setupDrag(g, n, nodeMap, edgeEls, nodeEls, nodeRadius)

    nodeG.appendChild(g)
    nodeEls.push({ g, circle, text, activeRing, node: n })
  }

  // Pan & zoom on SVG background
  setupPanZoom(svg, rootG)

  // Animated force simulation
  let alpha = 1.0
  function tick() {
    alpha *= 0.985
    if (alpha < 0.005) { graphAnimFrame = 0; return }

    // Repulsion (Barnes–Hut could optimize, but O(n²) is fine for < 500 nodes)
    for (let i = 0; i < graphNodes.length; i++) {
      if (graphNodes[i].pinned) continue
      for (let j = i + 1; j < graphNodes.length; j++) {
        let dx = graphNodes[j].x - graphNodes[i].x
        let dy = graphNodes[j].y - graphNodes[i].y
        const dist = Math.sqrt(dx * dx + dy * dy) || 1
        const force = (200 * alpha) / (dist * dist) * 60
        const fx = (dx / dist) * force
        const fy = (dy / dist) * force
        if (!graphNodes[i].pinned) { graphNodes[i].vx -= fx; graphNodes[i].vy -= fy }
        if (!graphNodes[j].pinned) { graphNodes[j].vx += fx; graphNodes[j].vy += fy }
      }
    }

    // Attraction along edges
    for (const edge of data.edges) {
      const s = nodeMap.get(edge.source)
      const t = nodeMap.get(edge.target)
      if (!s || !t) continue
      const dx = t.x - s.x
      const dy = t.y - s.y
      const dist = Math.sqrt(dx * dx + dy * dy) || 1
      const force = (dist - 120) * 0.005 * alpha
      const fx = (dx / dist) * force
      const fy = (dy / dist) * force
      if (!s.pinned) { s.vx += fx; s.vy += fy }
      if (!t.pinned) { t.vx -= fx; t.vy -= fy }
    }

    // Center gravity
    for (const n of graphNodes) {
      if (n.pinned) continue
      n.vx += (width / 2 - n.x) * 0.001 * alpha
      n.vy += (height / 2 - n.y) * 0.001 * alpha
    }

    // Apply velocity
    for (const n of graphNodes) {
      if (n.pinned) continue
      n.vx *= 0.6
      n.vy *= 0.6
      n.x += n.vx
      n.y += n.vy
    }

    // Update SVG positions
    for (const { g, node } of nodeEls) {
      g.setAttribute('transform', `translate(${node.x},${node.y})`)
    }
    for (const e of edgeEls) {
      const s = nodeMap.get(e.source)
      const t = nodeMap.get(e.target)
      if (s && t) {
        setEdgePositions(e.line, s, t, nodeRadius)
      }
    }

    graphAnimFrame = requestAnimationFrame(tick)
  }

  // Initial positions before first paint
  for (const { g, node } of nodeEls) {
    g.setAttribute('transform', `translate(${node.x},${node.y})`)
  }
  for (const e of edgeEls) {
    const s = nodeMap.get(e.source)
    const t = nodeMap.get(e.target)
    if (s && t) {
      setEdgePositions(e.line, s, t, nodeRadius)
    }
  }

  // Store node and edge refs for search and arrow toggle
  graphNodeElsRef = nodeEls
  graphEdgeElsRef = edgeEls.map(e => ({ line: e.line, source: e.source, target: e.target, bidir: e.bidir }))
  graphAdjacencyRef = adjacency
  
  graphAnimFrame = requestAnimationFrame(tick)
  graphReady.value = true
}

// Set edge line positions, shortened to stop at node circle boundary so arrows are visible
function setEdgePositions(line: SVGLineElement, s: GNode, t: GNode, nodeRadius: (n: GNode) => number) {
  const dx = t.x - s.x
  const dy = t.y - s.y
  const dist = Math.sqrt(dx * dx + dy * dy) || 1
  const ux = dx / dist
  const uy = dy / dist

  // Shorten each end by the node radius + arrow margin
  const rS = nodeRadius(s) + 4
  const rT = nodeRadius(t) + 4

  line.setAttribute('x1', String(s.x + ux * rS))
  line.setAttribute('y1', String(s.y + uy * rS))
  line.setAttribute('x2', String(t.x - ux * rT))
  line.setAttribute('y2', String(t.y - uy * rT))
}

// ─── Drag ───
function setupDrag(
  g: SVGGElement, node: GNode,
  nodeMap: Map<string, GNode>,
  edgeEls: { line: SVGLineElement; source: string; target: string; bidir: boolean }[],
  nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; activeRing: SVGCircleElement; node: GNode }[],
  nodeRadius: (n: GNode) => number,
) {
  let dragging = false
  let startX = 0, startY = 0

  function getPoint(e: MouseEvent | Touch) {
    const svg = graphSvg
    if (!svg) return { x: e.clientX, y: e.clientY }
    const pt = svg.createSVGPoint()
    pt.x = e.clientX; pt.y = e.clientY
    const rootG = svg.querySelector('.graph-root') as SVGGElement
    const ctm = rootG?.getCTM()?.inverse()
    if (ctm) {
      const svgP = pt.matrixTransform(ctm)
      return { x: svgP.x, y: svgP.y }
    }
    return { x: e.clientX, y: e.clientY }
  }

  function onStart(e: MouseEvent) {
    if (e.button !== 0) return
    e.stopPropagation()
    dragging = true
    node.pinned = true
    const p = getPoint(e)
    startX = p.x - node.x
    startY = p.y - node.y
    g.querySelector('circle')?.setAttribute('stroke', nodeColorMap[node.type] || '#8c8c8c')
    g.querySelector('circle')?.setAttribute('stroke-width', '3')
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onEnd)
  }

  function onMove(e: MouseEvent) {
    if (!dragging) return
    const p = getPoint(e)
    node.x = p.x - startX
    node.y = p.y - startY
    node.vx = 0; node.vy = 0
    g.setAttribute('transform', `translate(${node.x},${node.y})`)
    // Update connected edges immediately
    for (const edge of edgeEls) {
      if (edge.source === node.slug || edge.target === node.slug) {
        const sn = nodeMap.get(edge.source)
        const tn = nodeMap.get(edge.target)
        if (sn && tn) setEdgePositions(edge.line, sn, tn, nodeRadius)
      }
    }
  }

  function onEnd() {
    dragging = false
    // Keep pinned after drag so the node stays where user placed it
    g.querySelector('circle')?.setAttribute('stroke', '#fff')
    g.querySelector('circle')?.setAttribute('stroke-width', '2')
    window.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onEnd)
  }

  g.addEventListener('mousedown', onStart)
}

// ─── Pan & Zoom ───
function setupPanZoom(svg: SVGSVGElement, rootG: SVGGElement) {
  let scale = 1
  let translateX = 0, translateY = 0
  let panning = false
  let panStartX = 0, panStartY = 0
  let dragStartX = 0, dragStartY = 0

  function applyTransform() {
    rootG.setAttribute('transform', `translate(${translateX},${translateY}) scale(${scale})`)
  }

  // Export methods for programmatic pan/zoom
  let animId = 0
  graphPanZoomRef = {
    setScale: (s: number) => { scale = s },
    setTranslate: (x: number, y: number) => { translateX = x; translateY = y },
    apply: applyTransform,
    getScale: () => scale,
    flyTo: (tx: number, ty: number, s?: number, duration = 400) => {
      cancelAnimationFrame(animId)
      const startX = translateX, startY = translateY, startScale = scale
      const targetScale = s || scale
      const startTime = performance.now()
      const animate = (time: number) => {
        let t = (time - startTime) / duration
        if (t > 1) t = 1
        const ease = 1 - Math.pow(1 - t, 3) // cubic ease out
        translateX = startX + (tx - startX) * ease
        translateY = startY + (ty - startY) * ease
        scale = startScale + (targetScale - startScale) * ease
        applyTransform()
        if (t < 1) animId = requestAnimationFrame(animate)
      }
      animId = requestAnimationFrame(animate)
    }
  }

  // Zoom with mouse wheel
  svg.addEventListener('wheel', (e) => {
    e.preventDefault()
    const zoomFactor = e.deltaY > 0 ? 0.92 : 1.08
    const newScale = Math.max(0.2, Math.min(5, scale * zoomFactor))

    // Zoom towards cursor
    const rect = svg.getBoundingClientRect()
    const cx = e.clientX - rect.left
    const cy = e.clientY - rect.top
    translateX = cx - (cx - translateX) * (newScale / scale)
    translateY = cy - (cy - translateY) * (newScale / scale)
    scale = newScale
    applyTransform()
  }, { passive: false })

  // Pan with mouse drag on background
  svg.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return
    // Only pan if clicking the SVG background, not a node
    if ((e.target as Element).tagName === 'svg' || (e.target as Element).tagName === 'SVG') {
      panning = true
      panStartX = e.clientX - translateX
      panStartY = e.clientY - translateY
      dragStartX = e.clientX
      dragStartY = e.clientY
      svg.style.cursor = 'grabbing'
    }
  })

  window.addEventListener('mousemove', (e) => {
    if (!panning) return
    translateX = e.clientX - panStartX
    translateY = e.clientY - panStartY
    applyTransform()
  })

  window.addEventListener('mouseup', (e) => {
    if (panning) {
      panning = false
      svg.style.cursor = 'default'
      
      // If we barely moved, consider it a click to clear selection
      const dx = e.clientX - dragStartX
      const dy = e.clientY - dragStartY
      if (Math.abs(dx) < 5 && Math.abs(dy) < 5) {
        if ((e.target as Element).tagName === 'svg' || (e.target as Element).tagName === 'SVG') {
          graphSelectedSlug.value = null
          graphDrawerVisible.value = false
          clearHighlight(graphNodeElsRef, graphEdgeElsRef)
        }
      }
    }
  })
}

// ─── Hover Highlight ───
function applyHighlight(
  slug: string,
  adjacency: Map<string, Set<string>>,
  nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; activeRing: SVGCircleElement; node: GNode }[],
  edgeEls: { line: SVGLineElement; source: string; target: string; bidir: boolean }[],
  hoverSlug?: string
) {
  const neighbors = adjacency.get(slug) || new Set()
  const hoverNeighbors = hoverSlug ? (adjacency.get(hoverSlug) || new Set()) : new Set()
  
  for (const { g, circle, activeRing, node } of nodeEls) {
    if (node.slug === slug) {
      circle.setAttribute('r', String(Math.max(6, Math.min(18, 6 + node.linkCount * 1.5)) + 3))
      circle.setAttribute('stroke-width', '3')
      g.style.opacity = '1'
    } else if (hoverSlug && node.slug === hoverSlug) {
      circle.setAttribute('r', String(Math.max(6, Math.min(18, 6 + node.linkCount * 1.5)) + 3))
      circle.setAttribute('stroke-width', '3')
      g.style.opacity = '1'
    } else if (neighbors.has(node.slug) || (hoverSlug && hoverNeighbors.has(node.slug))) {
      circle.setAttribute('r', String(Math.max(6, Math.min(18, 6 + node.linkCount * 1.5))))
      circle.setAttribute('stroke-width', '2')
      g.style.opacity = '1'
    } else {
      circle.setAttribute('r', String(Math.max(6, Math.min(18, 6 + node.linkCount * 1.5))))
      circle.setAttribute('stroke-width', '2')
      g.style.opacity = '0.2'
    }
    
    if (node.slug === graphSelectedSlug.value) {
      activeRing.style.opacity = '1'
    } else {
      activeRing.style.opacity = '0'
    }
  }
  for (const e of edgeEls) {
    if (e.source === slug || e.target === slug || (hoverSlug && (e.source === hoverSlug || e.target === hoverSlug))) {
      e.line.setAttribute('stroke-opacity', '0.9')
      e.line.setAttribute('stroke-width', '2')
      
      // Determine which node is driving the highlight color
      const focusSlug = (hoverSlug && (e.source === hoverSlug || e.target === hoverSlug)) ? hoverSlug : slug
      const hlColor = nodeColorMap[
        nodeEls.find(n => n.node.slug === focusSlug)?.node.type || ''
      ] || '#0052d9'
      
      e.line.setAttribute('stroke', hlColor)
      e.line.setAttribute('marker-end', 'url(#arrow-end-hl)')
      if (e.bidir) e.line.setAttribute('marker-start', 'url(#arrow-start-hl)')
    } else {
      e.line.setAttribute('stroke-opacity', '0.08')
      e.line.setAttribute('stroke-width', '1')
      e.line.setAttribute('marker-end', 'url(#arrow-end)')
      if (e.bidir) e.line.setAttribute('marker-start', 'url(#arrow-start)')
      else e.line.removeAttribute('marker-start')
    }
  }
}

function clearHighlight(
  nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; activeRing: SVGCircleElement; node: GNode }[],
  edgeEls: { line: SVGLineElement; source: string; target: string; bidir: boolean }[],
) {
  if (graphSelectedSlug.value) {
    applyHighlight(graphSelectedSlug.value, graphAdjacencyRef, nodeEls, edgeEls)
    return
  }

  for (const { g, circle, activeRing, node } of nodeEls) {
    circle.setAttribute('r', String(Math.max(6, Math.min(18, 6 + node.linkCount * 1.5))))
    circle.setAttribute('stroke-width', '2')
    g.style.opacity = '1'
    activeRing.style.opacity = '0'
  }
  for (const e of edgeEls) {
    e.line.setAttribute('stroke', '#c0c4cc')
    e.line.setAttribute('stroke-width', '1.2')
    e.line.setAttribute('stroke-opacity', '0.4')
    e.line.setAttribute('marker-end', 'url(#arrow-end)')
    if (e.bidir) e.line.setAttribute('marker-start', 'url(#arrow-start)')
    else e.line.removeAttribute('marker-start')
  }
}

const graphSearchOptions = computed(() => {
  if (!graphData.value?.nodes) return []
  return graphData.value.nodes.map(n => ({
    label: n.title,
    value: n.slug
  }))
})

let graphNodeElsRef: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; activeRing: SVGCircleElement; node: GNode }[] = []
let graphEdgeElsRef: { line: SVGLineElement; source: string; target: string; bidir: boolean }[] = []
let graphAdjacencyRef = new Map<string, Set<string>>()

function handleGraphSearchSelect(value: string) {
  if (!value) return
  
  // Find node coordinates
  const node = graphNodes.find(n => n.slug === value)
  if (node && graphPanZoomRef) {
    const container = graphRef.value
    if (container) {
      const width = container.clientWidth
      const height = container.clientHeight
      // Center node while maintaining current scale, shifted left by 240px to account for the 480px drawer
      const currentScale = graphPanZoomRef.getScale()
      graphPanZoomRef.flyTo(
        width / 2 - node.x * currentScale - 240,
        height / 2 - node.y * currentScale
      )
    }
  }

  // Trigger highlight
  graphSelectedSlug.value = value
  graphHighlightSlug.value = value
  if (graphNodeElsRef.length > 0) {
    applyHighlight(value, graphAdjacencyRef, graphNodeElsRef, graphEdgeElsRef)
  }

  // Open drawer automatically when searching
  openGraphDrawer(value)

  // Clear search input after selection to be ready for next search
  setTimeout(() => { graphSearchValue.value = '' }, 300)
}

function handleGraphSearchEnter(context: { inputValue: string }) {
  const value = context.inputValue?.trim()
  if (!value) return
  
  // Try to find exact or partial match
  const match = graphSearchOptions.value.find(opt => 
    opt.label.toLowerCase().includes(value.toLowerCase()) || 
    opt.value.toLowerCase().includes(value.toLowerCase())
  )
  
  if (match) {
    handleGraphSearchSelect(match.value)
  }
}

// Load graph when switching to graph view
// Reload all pages when search query is cleared (backspace or clear button)
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(searchQuery, (val) => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    if (!val || !val.trim()) {
      loadPages()
    } else {
      doSearch()
    }
  }, 300)
})

watch(() => props.view, (v) => {
  if (v === 'graph') {
    loadGraph()
  } else if (v === 'browser') {
    nextTick(async () => {
      if (readerBodyRef.value && renderedContent.value) {
        await hydrateProtectedFileImages(readerBodyRef.value)
      }
    })
  }
})

onMounted(() => {
  loadPages()
  loadStats()
  if (props.view === 'graph') loadGraph()
})
</script>

<style scoped lang="less">
.wiki-browser {
  display: flex;
  height: 100%;
  min-height: 0;
  background: var(--td-bg-color-container);
}

// ── Left Sidebar ──
.wiki-sidebar {
  width: 280px;
  min-width: 240px;
  border-right: 1px solid var(--td-component-stroke);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  background: var(--td-bg-color-container);
}

.wiki-sidebar-header {
  padding: 16px 16px 12px;
}

.wiki-page-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 12px 12px;
}

.wiki-nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 4px;
  transition: all 0.15s;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &.active {
    background: var(--td-brand-color-light);
    .wiki-nav-text {
      color: var(--td-brand-color);
      font-weight: 600;
    }
    .wiki-nav-icon {
      color: var(--td-brand-color);
    }
  }

  .wiki-nav-icon {
    font-size: 16px;
    color: var(--td-text-color-secondary);
  }

  .wiki-nav-text {
    font-size: 14px;
    font-weight: 500;
    color: var(--td-text-color-primary);
  }
}

.wiki-sidebar-divider {
  height: 1px;
  background: var(--td-component-stroke);
  margin: 8px 12px;
}

.wiki-group-label {
  position: sticky;
  top: 0;
  z-index: 10;
  background: var(--td-bg-color-container);
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-secondary);
  padding: 12px 8px 8px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  user-select: none;
  transition: color 0.15s;

  &:hover {
    color: var(--td-text-color-primary);
  }

  &:first-child {
    margin-top: 0;
  }
}

.wiki-group-chevron {
  font-size: 14px;
  color: var(--td-text-color-placeholder);
  transition: transform 0.2s;
  flex-shrink: 0;
}

.wiki-group-count {
  margin-left: auto;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 10px;
  padding: 0 8px;
  line-height: 18px;
  text-align: center;
}

.wiki-page-item {
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 2px;
  transition: all 0.15s;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &.active {
    background: var(--td-brand-color-light);
  }
}

.wiki-page-item-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-bottom: 4px;
}

.wiki-page-item-summary {
  font-size: 12px;
  color: var(--td-text-color-secondary);
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  margin-bottom: 6px;
}

.wiki-page-item-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  color: var(--td-text-color-placeholder);
}

// ── Right Content ──
.wiki-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  overflow: hidden;
}

.wiki-reader {
  flex: 1;
  overflow-y: auto;
  padding: 16px 24px;
}

.wiki-reader-inner {
  width: 100%;
}

.wiki-reader-header {
  margin-bottom: 16px;
}

.wiki-nav-bar {
  margin-bottom: 16px;
}

.wiki-nav-back {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  text-decoration: none;
  padding: 4px 8px;
  margin-left: -8px;
  border-radius: 4px;
  transition: all 0.15s;

  &:hover {
    color: var(--td-brand-color);
    background: var(--td-bg-color-container-hover);
  }
}

.wiki-reader-title {
  margin: 0 0 12px;
  font-size: 26px;
  font-weight: 600;
  line-height: 1.3;
  color: var(--td-text-color-primary);
}

.wiki-reader-meta {
  display: flex;
  align-items: center;
  gap: 12px;
}

.wiki-reader-meta-text {
  font-size: 13px;
  color: var(--td-text-color-placeholder);
}

.wiki-reader-links {
  padding: 12px 16px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 8px;
  margin-bottom: 20px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.wiki-link-group {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 13px;
}

.wiki-link-label {
  color: var(--td-text-color-secondary);
  font-weight: 500;
  flex-shrink: 0;
}

.wiki-link-tag {
  color: var(--td-brand-color);
  text-decoration: none;
  font-family: monospace;
  font-size: 12px;
  padding: 2px 8px;
  background: rgba(7, 192, 95, 0.06);
  border-radius: 4px;
  transition: background 0.15s;

  &:hover {
    background: rgba(7, 192, 95, 0.12);
  }
}

.wiki-reader-body {
  line-height: 1.6;
  font-size: 14px;
  color: var(--td-text-color-primary);

  :deep(h1) { font-size: 24px; margin: 28px 0 16px; font-weight: 600; line-height: 1.4; }
  :deep(h2) { font-size: 18px; margin: 24px 0 12px; font-weight: 600; line-height: 1.4; }
  :deep(h3) { font-size: 16px; margin: 20px 0 10px; font-weight: 600; line-height: 1.5; }
  :deep(h4), :deep(h5), :deep(h6) { font-size: 14px; margin: 16px 0 8px; font-weight: 600; line-height: 1.5; }
  
  :deep(p) { margin: 0 0 14px; }
  
  :deep(ul), :deep(ol) { 
    margin: 0 0 14px; 
    padding-left: 24px; 
  }
  :deep(li) { 
    margin-bottom: 6px; 
    line-height: 1.6;
  }
  :deep(li > p) {
    margin-bottom: 6px;
  }

  :deep(blockquote) {
    margin: 0 0 14px;
    padding: 10px 16px;
    background: var(--td-bg-color-secondarycontainer);
    border-left: 4px solid var(--td-component-border);
    border-radius: 0 4px 4px 0;
    color: var(--td-text-color-secondary);
  }
  
  :deep(code) {
    font-family: monospace;
    font-size: 13px;
    padding: 2px 4px;
    background: var(--td-bg-color-secondarycontainer);
    border-radius: 4px;
    color: var(--td-brand-color);
  }
  
  :deep(pre) {
    margin: 0 0 14px;
    padding: 12px 16px;
    background: var(--td-bg-color-secondarycontainer);
    border-radius: 6px;
    overflow-x: auto;
    
    code {
      padding: 0;
      background: transparent;
      color: inherit;
    }
  }

  :deep(p:has(img)) {
    text-align: center;
    color: var(--td-text-color-secondary);
    font-size: 13px;
    margin-top: 16px;
    margin-bottom: 24px;
    
    img {
      max-width: 100%;
      max-height: 400px;
      object-fit: contain;
      border-radius: 6px;
      display: block;
      margin: 0 auto 8px;
      cursor: zoom-in;
      transition: opacity 0.2s;
      
      &:hover {
        opacity: 0.9;
      }
    }
  }

  :deep(.wiki-content-link) {
    color: var(--td-brand-color);
    text-decoration: none;
    border-bottom: 1px dashed var(--td-brand-color);
    cursor: pointer;
    font-weight: 500;
    &:hover { border-bottom-style: solid; }
  }
}

.wiki-reader-backlinks {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--td-component-stroke);
  margin-bottom: 24px;
}

.wiki-backlink-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 13px;
  color: var(--td-text-color-placeholder);
  font-weight: 500;
  flex-shrink: 0;
  margin-right: 4px;
}

.wiki-backlink-tag {
  color: var(--td-text-color-secondary);
  text-decoration: none;
  font-size: 13px;
  padding: 2px 8px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 4px;
  transition: all 0.15s;

  &:hover {
    color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }
}

.wiki-reader-sources {
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--td-component-stroke);
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 13px;
}

.wiki-source-ref {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 10px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 4px;
  color: var(--td-brand-color);
  font-size: 12px;
  text-decoration: none;
  cursor: pointer;
  transition: background 0.15s;

  &:hover {
    background: var(--td-brand-color-light);
  }
}

// ── Empty states ──
.wiki-empty-state,
.wiki-reader-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  text-align: center;
}

.wiki-empty-icon {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: var(--td-bg-color-secondarycontainer);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 16px;
  color: var(--td-text-color-placeholder);
}

.wiki-empty-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--td-text-color-secondary);
  margin: 0 0 4px;
}

.wiki-empty-desc {
  font-size: 13px;
  color: var(--td-text-color-placeholder);
  margin: 0;
}

// ── Graph ──
.wiki-graph {
  flex: 1;
  position: relative;
  overflow: hidden;
  width: 100%;
  height: 100%;
}

.wiki-graph-search {
  position: absolute;
  top: 16px;
  left: 16px;
  width: 280px;
  z-index: 10;
  box-shadow: var(--td-shadow-1);
  border-radius: 4px;
}

:deep(.wiki-graph-drawer) {
  box-shadow: -4px 0 16px rgba(0, 0, 0, 0.08);
}

.graph-search-select {
  background: var(--td-bg-color-container) !important;
  opacity: 0.95;
}

.wiki-graph-canvas {
  width: 100%;
  height: 100%;
  min-height: 500px;
}

.wiki-graph-legend {
  position: absolute;
  top: 16px;
  right: 16px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
  padding: 10px 12px;
  box-shadow: var(--td-shadow-1);
  display: flex;
  flex-direction: column;
  gap: 12px;
  z-index: 10;
  opacity: 0.95;
}

.legend-items {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  color: var(--td-text-color-secondary);
}

.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  display: inline-block;
  flex-shrink: 0;
}

.legend-divider {
  height: 1px;
  background: var(--td-component-stroke);
  margin: 0 -12px;
}

.legend-action {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  user-select: none;
  transition: all 0.15s;

  &:hover {
    color: var(--td-brand-color);
    .legend-action-icon {
      color: var(--td-brand-color);
    }
  }
  
  &.active {
    color: var(--td-brand-color);
    .legend-action-icon {
      color: var(--td-brand-color);
    }
  }
}

.legend-action-icon {
  font-size: 13px;
  font-family: monospace;
  color: var(--td-text-color-placeholder);
  transition: color 0.15s;
}

@keyframes node-active-pulse {
  0% { transform: scale(1); opacity: 0.8; }
  100% { transform: scale(1.6); opacity: 0; }
}

.node-active-ring {
  transform-origin: 0 0;
  animation: node-active-pulse 1.5s cubic-bezier(0.25, 0.46, 0.45, 0.94) infinite;
}
</style>
