export interface WikiConflictComparisonItem {
  change_category?: string
  before?: Record<string, unknown>
  after?: Record<string, any>
}

interface ConflictExistingEntry {
  title: string
  evidence: string
  claims: string[]
}

function textValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function storedPageContent(page?: Record<string, unknown>): string {
  if (!page) return ''
  return textValue(page.content) || textValue(page.summary)
}

function evidenceContent(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (!value || typeof value !== 'object' || Array.isArray(value)) return ''
  const evidence = value as Record<string, unknown>
  return textValue(evidence.excerpt)
    || textValue(evidence.content)
    || textValue(evidence.summary)
    || textValue(evidence.text)
}

function conflictTitle(assessment: Record<string, any>, evidence: Record<string, any>): string {
  const slug = textValue(assessment.related_slug)
  const slugTail = slug.split('/').filter(Boolean).pop()?.replace(/[-_]+/g, ' ')
  return textValue(evidence.title) || textValue(assessment.related_title) || slugTail || '相关知识'
}

export function crossPageConflictExistingContent(item: WikiConflictComparisonItem): string {
  if (item.change_category !== 'conflict') return ''

  const metadata = item.after?.page_metadata
  const assessments = Array.isArray(metadata?.cross_page_assessments) ? metadata.cross_page_assessments : []
  const relatedEvidence = metadata?.related_page_evidence && typeof metadata.related_page_evidence === 'object'
    ? metadata.related_page_evidence as Record<string, unknown>
    : {}

  const entriesBySlug = new Map<string, ConflictExistingEntry>()
  assessments.forEach((rawAssessment: unknown) => {
    if (!rawAssessment || typeof rawAssessment !== 'object' || Array.isArray(rawAssessment)) return []
    const assessment = rawAssessment as Record<string, any>
    if (assessment.relation !== 'conflicting' && assessment.relation !== 'supersedes') return []

    const slug = textValue(assessment.related_slug)
    const rawEvidence = slug ? relatedEvidence[slug] : undefined
    const evidence = rawEvidence && typeof rawEvidence === 'object' && !Array.isArray(rawEvidence)
      ? rawEvidence as Record<string, any>
      : {}
    const claim = textValue(assessment.existing_claim)
    const excerpt = evidenceContent(rawEvidence)
    if (!claim && !excerpt) return

    const key = slug || `assessment-${entriesBySlug.size}`
    const entry = entriesBySlug.get(key) || {
      title: conflictTitle(assessment, evidence),
      evidence: excerpt,
      claims: [],
    }
    if (claim && !entry.claims.includes(claim)) entry.claims.push(claim)
    if (!entry.evidence && excerpt) entry.evidence = excerpt
    entriesBySlug.set(key, entry)
  })

  return Array.from(entriesBySlug.values()).map(entry => {
    const sections = [`## ${entry.title}`]
    if (entry.claims.length === 1) sections.push(`**发生冲突的说法**\n\n${entry.claims[0]}`)
    if (entry.claims.length > 1) sections.push(`**发生冲突的说法**\n\n${entry.claims.map(claim => `- ${claim}`).join('\n')}`)
    if (entry.evidence) sections.push(`### 原知识页面内容\n\n${entry.evidence}`)
    return sections.join('\n\n')
  }).join('\n\n')
}

export function wikiComparisonBeforeContent(item: WikiConflictComparisonItem): string {
  return storedPageContent(item.before) || crossPageConflictExistingContent(item)
}

export function usesCrossPageConflictFallback(item: WikiConflictComparisonItem): boolean {
  return !storedPageContent(item.before) && Boolean(crossPageConflictExistingContent(item))
}
