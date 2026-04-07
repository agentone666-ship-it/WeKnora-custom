<template>
  <div class="wiki-browser">
    <!-- Graph view (full screen) -->
    <template v-if="view === 'graph'">
      <div class="wiki-graph">
        <div ref="graphRef" class="wiki-graph-canvas"></div>
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
        >
          <template v-if="graphDrawerPage">
            <div class="wiki-reader-meta" style="margin-bottom: 16px;">
              <t-tag size="small" :theme="getTypeTheme(graphDrawerPage.page_type)" variant="light-outline">
                {{ getTypeLabel(graphDrawerPage.page_type) }}
              </t-tag>
              <span class="wiki-reader-meta-text">{{ $t('knowledgeEditor.wikiBrowser.version', { ver: graphDrawerPage.version }) }}</span>
            </div>
            <div class="wiki-reader-body" v-html="graphDrawerContent" @click="handleGraphDrawerClick"></div>
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
            size="small"
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
            :class="['wiki-page-item wiki-page-item-index', { active: selectedPage?.id === indexPage.id }]"
            @click="selectPage(indexPage)"
          >
            <div class="wiki-page-item-title">
              <t-icon name="catalog" size="14px" class="wiki-index-icon" />
              {{ indexPage.title || 'Index' }}
            </div>
          </div>

          <!-- Log page (pinned) -->
          <div
            v-if="logPage"
            :class="['wiki-page-item wiki-page-item-log', { active: selectedPage?.id === logPage.id }]"
            @click="selectPage(logPage)"
          >
            <div class="wiki-page-item-title">
              <t-icon name="history" size="14px" class="wiki-index-icon" />
              {{ $t('knowledgeEditor.wikiBrowser.logTitle') }}
            </div>
          </div>

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
                  <span class="wiki-page-item-slug">{{ page.slug }}</span>
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

        <!-- Stats footer -->
        <div v-if="stats" class="wiki-sidebar-footer">
          {{ $t('knowledgeEditor.wikiBrowser.stats', { pages: stats.total_pages, links: stats.total_links }) }}
        </div>
      </aside>

      <!-- Right Panel: Reader -->
      <div class="wiki-content">
        <div class="wiki-reader">
          <template v-if="selectedPage">
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
            <div class="wiki-reader-body" v-html="renderedContent" @click="handleContentClick"></div>

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
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
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
const graphRef = ref<HTMLElement | null>(null)
const loading = ref(false)
const graphLoading = ref(false)
const graphReady = ref(false)
const showArrows = ref(true)
const graphEdgeEls = ref<{ line: SVGLineElement; bidir: boolean }[]>([])
const collapsedGroups = reactive<Record<string, boolean>>({})
const graphDrawerVisible = ref(false)
const graphDrawerPage = ref<WikiPage | null>(null)
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

function renderMarkdown(content: string): string {
  let html = content
  html = html.replace(/\[\[([^\]]+)\]\]/g, (_, slug) =>
    `<a href="#" class="wiki-content-link" data-slug="${slug}">[[${slug}]]</a>`)
  html = html.replace(/^### (.+)$/gm, '<h3>$1</h3>')
  html = html.replace(/^## (.+)$/gm, '<h2>$1</h2>')
  html = html.replace(/^# (.+)$/gm, '<h1>$1</h1>')
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>')
  html = html.replace(/^- (.+)$/gm, '<li>$1</li>')
  html = html.replace(/\n\n/g, '</p><p>')
  html = html.replace(/\n/g, '<br>')
  return `<p>${html}</p>`
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
    if (slug) openGraphDrawer(slug)
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

function handleContentClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (target.classList.contains('wiki-content-link')) {
    e.preventDefault()
    const slug = target.getAttribute('data-slug')
    if (slug) navigateToSlug(slug)
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
    const res = await getWikiPage(props.knowledgeBaseId, page.slug)
    selectedPage.value = (res as any).data || res as any
  } catch (e) {
    console.error('Failed to load wiki page:', e)
  }
}

async function navigateToSlug(slug: string) {
  try {
    const res = await getWikiPage(props.knowledgeBaseId, slug)
    selectedPage.value = (res as any).data || res as any
  } catch (e) {
    console.error(`Failed to navigate to ${slug}:`, e)
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
  for (const e of graphEdgeEls.value) {
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

const graphHighlightSlug = ref<string | null>(null)

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
  arrowPath.setAttribute('d', 'M0,0 L10,3 L0,6')
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
  arrowPathStart.setAttribute('d', 'M10,0 L0,3 L10,6')
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
    p.setAttribute('d', id.includes('end') ? 'M0,0 L10,3 L0,6' : 'M10,0 L0,3 L10,6')
    p.setAttribute('fill', '#0052d9')
    m.appendChild(p)
    defs.appendChild(m)
  }

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
    if (bidir) {
      line.setAttribute('marker-start', 'url(#arrow-start)')
    }
    edgeG.appendChild(line)
    edgeEls.push({ line, source: edge.source, target: edge.target, bidir })
  }

  // Create SVG elements for nodes
  const nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; node: GNode }[] = []
  for (const n of graphNodes) {
    const g = document.createElementNS('http://www.w3.org/2000/svg', 'g')
    g.style.cursor = 'pointer'

    const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
    const r = nodeRadius(n)
    circle.setAttribute('r', String(r))
    circle.setAttribute('fill', nodeColorMap[n.type] || '#8c8c8c')
    circle.setAttribute('stroke', '#fff')
    circle.setAttribute('stroke-width', '2')
    circle.style.transition = 'r 0.2s, stroke-width 0.2s, opacity 0.2s'
    g.appendChild(circle)

    const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
    text.setAttribute('text-anchor', 'middle')
    text.setAttribute('dy', String(r + 14))
    text.setAttribute('font-size', '11')
    text.setAttribute('fill', 'var(--td-text-color-secondary)')
    text.setAttribute('pointer-events', 'none')
    text.textContent = n.title.length > 14 ? n.title.substring(0, 14) + '…' : n.title
    g.appendChild(text)

    // Hover highlight
    g.addEventListener('mouseenter', () => {
      graphHighlightSlug.value = n.slug
      applyHighlight(n.slug, adjacency, nodeEls, edgeEls)
    })
    g.addEventListener('mouseleave', () => {
      graphHighlightSlug.value = null
      clearHighlight(nodeEls, edgeEls)
    })

    // Click to open drawer
    g.addEventListener('click', (e) => {
      e.stopPropagation()
      openGraphDrawer(n.slug)
    })

    // Drag support
    setupDrag(g, n, nodeMap, edgeEls, nodeEls, nodeRadius)

    nodeG.appendChild(g)
    nodeEls.push({ g, circle, text, node: n })
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

  // Draw legend
  drawLegend(svg, width)

  // Store edge refs for arrow toggle
  graphEdgeEls.value = edgeEls.map(e => ({ line: e.line, bidir: e.bidir }))

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
  nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; node: GNode }[],
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

  function applyTransform() {
    rootG.setAttribute('transform', `translate(${translateX},${translateY}) scale(${scale})`)
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
      svg.style.cursor = 'grabbing'
    }
  })

  window.addEventListener('mousemove', (e) => {
    if (!panning) return
    translateX = e.clientX - panStartX
    translateY = e.clientY - panStartY
    applyTransform()
  })

  window.addEventListener('mouseup', () => {
    if (panning) {
      panning = false
      svg.style.cursor = 'default'
    }
  })
}

// ─── Hover Highlight ───
function applyHighlight(
  slug: string,
  adjacency: Map<string, Set<string>>,
  nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; node: GNode }[],
  edgeEls: { line: SVGLineElement; source: string; target: string; bidir: boolean }[],
) {
  const neighbors = adjacency.get(slug) || new Set()
  for (const { g, circle, node } of nodeEls) {
    if (node.slug === slug) {
      circle.setAttribute('r', String(Math.max(6, Math.min(18, 6 + node.linkCount * 1.5)) + 3))
      circle.setAttribute('stroke-width', '3')
      g.style.opacity = '1'
    } else if (neighbors.has(node.slug)) {
      g.style.opacity = '1'
    } else {
      g.style.opacity = '0.2'
    }
  }
  for (const e of edgeEls) {
    if (e.source === slug || e.target === slug) {
      e.line.setAttribute('stroke-opacity', '0.9')
      e.line.setAttribute('stroke-width', '2')
      const hlColor = nodeColorMap[
        nodeEls.find(n => n.node.slug === slug)?.node.type || ''
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
  nodeEls: { g: SVGGElement; circle: SVGCircleElement; text: SVGTextElement; node: GNode }[],
  edgeEls: { line: SVGLineElement; source: string; target: string; bidir: boolean }[],
) {
  for (const { g, circle, node } of nodeEls) {
    circle.setAttribute('r', String(Math.max(6, Math.min(18, 6 + node.linkCount * 1.5))))
    circle.setAttribute('stroke-width', '2')
    g.style.opacity = '1'
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

// ─── Legend ───
function drawLegend(svg: SVGSVGElement, width: number) {
  const legendG = document.createElementNS('http://www.w3.org/2000/svg', 'g')
  legendG.setAttribute('transform', `translate(${width - 140}, 16)`)

  // Background
  const bg = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
  bg.setAttribute('x', '-8'); bg.setAttribute('y', '-4')
  bg.setAttribute('width', '132'); bg.setAttribute('rx', '6')
  bg.setAttribute('fill', 'var(--td-bg-color-container)')
  bg.setAttribute('fill-opacity', '0.9')
  bg.setAttribute('stroke', 'var(--td-component-stroke)')
  bg.setAttribute('stroke-width', '1')
  legendG.appendChild(bg)

  const types = [
    { type: 'summary', label: t('knowledgeEditor.wikiBrowser.filterSummary') },
    { type: 'entity', label: t('knowledgeEditor.wikiBrowser.filterEntity') },
    { type: 'concept', label: t('knowledgeEditor.wikiBrowser.filterConcept') },
    { type: 'synthesis', label: t('knowledgeEditor.wikiBrowser.filterSynthesis') },
    { type: 'comparison', label: t('knowledgeEditor.wikiBrowser.filterComparison') },
  ]

  let y = 10
  for (const { type, label } of types) {
    const dot = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
    dot.setAttribute('cx', '6'); dot.setAttribute('cy', String(y))
    dot.setAttribute('r', '5'); dot.setAttribute('fill', nodeColorMap[type])
    legendG.appendChild(dot)

    const txt = document.createElementNS('http://www.w3.org/2000/svg', 'text')
    txt.setAttribute('x', '18'); txt.setAttribute('y', String(y + 4))
    txt.setAttribute('font-size', '11')
    txt.setAttribute('fill', 'var(--td-text-color-secondary)')
    txt.textContent = label
    legendG.appendChild(txt)

    y += 22
  }

  // Divider line
  y += 4
  const divider = document.createElementNS('http://www.w3.org/2000/svg', 'line')
  divider.setAttribute('x1', '-2'); divider.setAttribute('x2', '118')
  divider.setAttribute('y1', String(y)); divider.setAttribute('y2', String(y))
  divider.setAttribute('stroke', 'var(--td-component-stroke)')
  divider.setAttribute('stroke-width', '1')
  legendG.appendChild(divider)
  y += 12

  // Arrow toggle row
  const arrowRow = document.createElementNS('http://www.w3.org/2000/svg', 'g')
  arrowRow.style.cursor = 'pointer'

  // Arrow icon (small →)
  const arrowIcon = document.createElementNS('http://www.w3.org/2000/svg', 'text')
  arrowIcon.setAttribute('x', '3'); arrowIcon.setAttribute('y', String(y + 3))
  arrowIcon.setAttribute('font-size', '13')
  arrowIcon.setAttribute('fill', showArrows.value ? 'var(--td-brand-color)' : 'var(--td-text-color-placeholder)')
  arrowIcon.textContent = '→'
  arrowRow.appendChild(arrowIcon)

  const arrowLabel = document.createElementNS('http://www.w3.org/2000/svg', 'text')
  arrowLabel.setAttribute('x', '18'); arrowLabel.setAttribute('y', String(y + 4))
  arrowLabel.setAttribute('font-size', '11')
  arrowLabel.setAttribute('fill', showArrows.value ? 'var(--td-brand-color)' : 'var(--td-text-color-secondary)')
  arrowLabel.textContent = showArrows.value
    ? t('knowledgeEditor.wikiBrowser.hideArrows')
    : t('knowledgeEditor.wikiBrowser.showArrows')
  arrowRow.appendChild(arrowLabel)

  arrowRow.addEventListener('click', () => {
    toggleArrows()
    // Update label & color after toggle
    arrowLabel.textContent = showArrows.value
      ? t('knowledgeEditor.wikiBrowser.hideArrows')
      : t('knowledgeEditor.wikiBrowser.showArrows')
    arrowLabel.setAttribute('fill', showArrows.value ? 'var(--td-brand-color)' : 'var(--td-text-color-secondary)')
    arrowIcon.setAttribute('fill', showArrows.value ? 'var(--td-brand-color)' : 'var(--td-text-color-placeholder)')
  })

  legendG.appendChild(arrowRow)
  y += 16

  bg.setAttribute('height', String(y + 4))
  svg.appendChild(legendG)
}

// Load graph when switching to graph view
// Reload all pages when search query is cleared (backspace or clear button)
watch(searchQuery, (val) => {
  if (!val || !val.trim()) {
    loadPages()
  }
})

watch(() => props.view, (v) => {
  if (v === 'graph') loadGraph()
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
  width: 300px;
  min-width: 260px;
  border-right: 1px solid var(--td-component-stroke);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.wiki-sidebar-header {
  padding: 16px 12px 12px;
}

.wiki-page-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 8px;
}

.wiki-page-item-index,
.wiki-page-item-log {
  .wiki-page-item-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
  }
  .wiki-index-icon {
    color: var(--td-text-color-placeholder);
  }
}

.wiki-group-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--td-text-color-placeholder);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding: 12px 12px 4px;
  margin-top: 4px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  user-select: none;

  &:hover {
    color: var(--td-text-color-secondary);
  }

  &:first-child {
    margin-top: 0;
  }
}

.wiki-group-chevron {
  flex-shrink: 0;
}

.wiki-group-count {
  margin-left: auto;
  font-size: 10px;
  font-weight: 400;
  color: var(--td-text-color-placeholder);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 8px;
  padding: 0 6px;
  min-width: 18px;
  text-align: center;
}

.wiki-page-item {
  padding: 8px 12px;
  border-radius: 8px;
  cursor: pointer;
  margin-bottom: 2px;
  transition: all 0.15s cubic-bezier(0.2, 0, 0, 1);

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &.active {
    background: var(--td-brand-color-light);
  }
}

.wiki-page-item-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--td-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-bottom: 2px;
}

.wiki-page-item-summary {
  font-size: 12px;
  color: var(--td-text-color-secondary);
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  margin-bottom: 4px;
}

.wiki-page-item-meta {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--td-text-color-placeholder);
}

.wiki-page-item-slug {
  font-family: monospace;
}

.wiki-sidebar-footer {
  padding: 8px 16px;
  border-top: 1px solid var(--td-component-stroke);
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  text-align: center;
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
  padding: 24px 32px;
}

.wiki-reader-header {
  margin-bottom: 24px;
}

.wiki-reader-title {
  margin: 0 0 8px;
  font-size: 22px;
  font-weight: 600;
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
  line-height: 1.8;
  font-size: 14px;
  color: var(--td-text-color-primary);

  :deep(h1) { font-size: 22px; margin: 24px 0 12px; font-weight: 600; }
  :deep(h2) { font-size: 18px; margin: 20px 0 10px; font-weight: 600; }
  :deep(h3) { font-size: 16px; margin: 16px 0 8px; font-weight: 500; }
  :deep(p) { margin: 0 0 12px; }
  :deep(li) { margin-left: 20px; margin-bottom: 4px; }

  :deep(.wiki-content-link) {
    color: var(--td-brand-color);
    text-decoration: none;
    font-family: monospace;
    font-size: 13px;
    padding: 1px 4px;
    background: rgba(7, 192, 95, 0.06);
    border-radius: 3px;
    cursor: pointer;
    &:hover { background: rgba(7, 192, 95, 0.12); }
  }
}

.wiki-reader-backlinks {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  padding: 10px 14px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 8px;
  margin-bottom: 16px;
}

.wiki-backlink-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  font-weight: 500;
  flex-shrink: 0;
  margin-right: 2px;
}

.wiki-backlink-tag {
  color: var(--td-brand-color);
  text-decoration: none;
  font-size: 12px;
  padding: 2px 8px;
  background: var(--td-bg-color-container);
  border-radius: 4px;
  transition: background 0.15s;

  &:hover {
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

.wiki-graph-canvas {
  width: 100%;
  height: 100%;
  min-height: 500px;
}
</style>
