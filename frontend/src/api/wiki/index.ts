import { get, post, put, del } from "../../utils/request";

// encodeSlugPath encodes each segment of a hierarchical wiki slug (e.g.
// "foo/bar baz?") so the URL is safe while preserving the "/" separators
// between segments. Using encodeURIComponent on the whole slug would also
// escape the "/" and break hierarchical routing on the backend.
function encodeSlugPath(slug: string): string {
  return slug.split("/").map(encodeURIComponent).join("/");
}

// Wiki Page Types
export interface WikiPage {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  slug: string;
  title: string;
  page_type: string;
  status: string;
  content: string;
  summary: string;
  aliases: string[];
  parent_slug?: string;
  category_path?: string[];
  wiki_path?: string;
  depth?: number;
  sort_order?: number;
  source_refs: string[];
  in_links: string[];
  out_links: string[];
  page_metadata: Record<string, any>;
  knowledge_type?: string;
  maturity_status?: string;
  answer_strength?: string;
  review_status?: string;
  business_line?: string;
  scenario_ids?: string[];
  audience_roles?: string[];
  affected_metrics?: string[];
  applicability?: Record<string, any>;
  prohibited_claims?: string[];
  reviewed_by?: string;
  reviewed_at?: string;
  effective_from?: string;
  effective_to?: string;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface WikiPageListResponse {
  pages: WikiPage[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export interface WikiFolder {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  parent_id: string;
  name: string;
  path: string;
  depth: number;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export interface WikiFolderNode extends WikiFolder {
  page_count: number;
  has_children: boolean;
}

export interface WikiFolderListResponse {
  parent_id: string;
  folders: WikiFolderNode[];
}

export interface WikiGraphMeta {
  mode: 'overview' | 'ego' | string;
  total: number;
  returned: number;
  truncated: boolean;
  center?: string;
  depth?: number;
}

export interface WikiGraphData {
  nodes: { slug: string; title: string; page_type: string; link_count: number }[];
  edges: { source: string; target: string }[];
  meta: WikiGraphMeta;
}

export interface WikiStats {
  total_pages: number;
  pages_by_type: Record<string, number>;
  total_links: number;
  orphan_count: number;
  recent_updates: WikiPage[];
  pending_tasks: number;
  pending_issues: number;
  is_active: boolean;
}

export interface WikiPageIssue {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  slug: string;
  issue_type: string;
  description: string;
  suspected_knowledge_ids: string[];
  status: string;
  reported_by: string;
  created_at: string;
  updated_at: string;
}

// Wiki API Functions
export function listWikiPages(kbId: string, params?: {
  page_type?: string;
  status?: string;
  query?: string;
  category_path?: string;
  category_depth?: number;
  page?: number;
  page_size?: number;
  sort_by?: string;
  sort_order?: string;
}) {
  const query = new URLSearchParams();
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== '') {
        query.set(key, String(value));
      }
    });
  }
  const qs = query.toString();
  return get(`/api/v1/knowledgebase/${kbId}/wiki/pages${qs ? '?' + qs : ''}`);
}

// listWikiFolders returns the direct child folders of parentId ("" = root),
// each enriched with a recursive page_count and a has_children flag so the tree
// can render expand affordances and empty folders without a second request.
// pageTypes scopes the view to a sidebar tab: only folders whose subtree holds
// a page of those types (or are entirely empty) come back, and page_count is
// counted within those types.
export function listWikiFolders(kbId: string, parentId = '', pageTypes = '') {
  const query = new URLSearchParams();
  if (parentId) query.set('parent_id', parentId);
  if (pageTypes) query.set('page_types', pageTypes);
  const qs = query.toString();
  return get(`/api/v1/knowledgebase/${kbId}/wiki/folders${qs ? '?' + qs : ''}`);
}

// createWikiFolder creates a new empty folder under parentId ("" = root).
export function createWikiFolder(kbId: string, parentId: string, name: string) {
  return post(`/api/v1/knowledgebase/${kbId}/wiki/folders`, { parent_id: parentId, name });
}

// updateWikiFolder renames and/or reparents a folder. Pass move_parent: true
// (and parent_id) to reparent; omit it for a pure rename.
export function updateWikiFolder(
  kbId: string,
  folderId: string,
  data: { name?: string; parent_id?: string; move_parent?: boolean },
) {
  return put(`/api/v1/knowledgebase/${kbId}/wiki/folders/${folderId}`, data);
}

// deleteWikiFolder removes an empty folder (no pages, no sub-folders).
export function deleteWikiFolder(kbId: string, folderId: string) {
  return del(`/api/v1/knowledgebase/${kbId}/wiki/folders/${folderId}`);
}

// moveWikiPage relocates a page into folderId ("" = root). The slug is sent in
// the body because wiki slugs are hierarchical.
export function moveWikiPage(kbId: string, slug: string, folderId: string) {
  return put(`/api/v1/knowledgebase/${kbId}/wiki/move-page`, { slug, folder_id: folderId });
}

export function createWikiPage(kbId: string, data: Partial<WikiPage>) {
  return post(`/api/v1/knowledgebase/${kbId}/wiki/pages`, data);
}

export function getWikiPage(kbId: string, slug: string) {
  return get(`/api/v1/knowledgebase/${kbId}/wiki/pages/${encodeSlugPath(slug)}`);
}

export function updateWikiPage(kbId: string, slug: string, data: Partial<WikiPage>) {
  return put(`/api/v1/knowledgebase/${kbId}/wiki/pages/${encodeSlugPath(slug)}`, data);
}

export function deleteWikiPage(kbId: string, slug: string) {
  return del(`/api/v1/knowledgebase/${kbId}/wiki/pages/${encodeSlugPath(slug)}`);
}

export interface WikiIndexEntryDTO {
  slug: string;
  title: string;
  summary: string;
  parent_slug?: string;
  category_path?: string[];
  wiki_path?: string;
  depth?: number;
  sort_order?: number;
}

export interface WikiIndexGroup {
  type: string;
  total: number;
  items: WikiIndexEntryDTO[];
  next_cursor?: string;
}

export interface WikiIndexResponse {
  intro: string;
  version: number;
  groups: WikiIndexGroup[];
}

// getWikiIndex fetches the structured index view for a wiki KB. The
// backend replaced the legacy "markdown blob of intro + directory" with
// { intro, groups } so a 40k-page wiki no longer round-trips multiple
// megabytes on every index open. Pass `types` to restrict which
// page_type buckets come back; `limit` bounds the per-group window;
// `cursor` resumes from a previous response.
export function getWikiIndex(
  kbId: string,
  params?: { types?: string[]; limit?: number; cursor?: string },
) {
  const query = new URLSearchParams();
  if (params) {
    if (params.types && params.types.length > 0) query.set('types', params.types.join(','));
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    if (params.cursor) query.set('cursor', params.cursor);
  }
  const qs = query.toString();
  const suffix = qs ? `?${qs}` : '';
  return get(`/api/v1/knowledgebase/${kbId}/wiki/index${suffix}`);
}

export interface WikiLogPageRef {
  slug: string;
  title?: string;
}

export interface WikiLogEntry {
  id: number;
  tenant_id: number;
  knowledge_base_id: string;
  action: string;
  knowledge_id: string;
  doc_title: string;
  summary: string;
  // Each ref carries both slug (for navigation) and title (captured at
  // ingest time for display). Legacy rows written before the title
  // column was added surface as refs with an empty title; render falls
  // back to the slug in that case.
  pages_affected: WikiLogPageRef[];
  created_at: string;
}

export interface WikiLogListResponse {
  entries: WikiLogEntry[];
  next_cursor?: string;
}

// getWikiLog fetches a page of wiki operation events (newest first). Pass the
// `next_cursor` from the previous response back as `cursor` to load more;
// an empty / missing `next_cursor` signals end-of-feed. `limit` is clamped
// server-side to [1, 200] and defaults to 50.
export function getWikiLog(kbId: string, params?: { cursor?: string; limit?: number }) {
  const query = new URLSearchParams();
  if (params) {
    if (params.cursor) query.set('cursor', params.cursor);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
  }
  const qs = query.toString();
  const suffix = qs ? `?${qs}` : '';
  return get(`/api/v1/knowledgebase/${kbId}/wiki/log${suffix}`);
}

export interface WikiGraphQueryParams {
  mode?: 'overview' | 'ego';
  center?: string;
  depth?: number;
  types?: string[];
  limit?: number;
}

// getWikiGraph fetches a slice of the wiki link graph. Without params the
// backend returns the top-500 most-connected pages (overview mode). Pass
// `mode: 'ego', center: <slug>` to drill into a specific page's neighborhood.
// For knowledge bases with tens of thousands of pages the overview cap is
// what prevents the browser from choking on a 30MB payload / 100k SVG nodes.
export function getWikiGraph(kbId: string, params?: WikiGraphQueryParams) {
  const query = new URLSearchParams();
  if (params) {
    if (params.mode) query.set('mode', params.mode);
    if (params.center) query.set('center', params.center);
    if (params.depth !== undefined) query.set('depth', String(params.depth));
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    if (params.types && params.types.length > 0) {
      query.set('types', params.types.join(','));
    }
  }
  const qs = query.toString();
  return get(`/api/v1/knowledgebase/${kbId}/wiki/graph${qs ? '?' + qs : ''}`);
}

export function getWikiStats(kbId: string) {
  return get(`/api/v1/knowledgebase/${kbId}/wiki/stats`);
}

export function searchWikiPages(kbId: string, q: string, limit?: number) {
  const params = new URLSearchParams({ q });
  if (limit) params.set('limit', String(limit));
  return get(`/api/v1/knowledgebase/${kbId}/wiki/search?${params.toString()}`);
}

export function listWikiIssues(kbId: string, slug?: string, status?: string) {
  const params = new URLSearchParams();
  if (slug) params.set('slug', slug);
  if (status) params.set('status', status);
  return get(`/api/v1/knowledgebase/${kbId}/wiki/issues?${params.toString()}`);
}

export function updateWikiIssueStatus(kbId: string, issueId: string, status: string) {
  return put(`/api/v1/knowledgebase/${kbId}/wiki/issues/${issueId}/status`, { status });
}

export function rebuildWikiLinks(kbId: string) {
  return post(`/api/v1/knowledgebase/${kbId}/wiki/rebuild-links`, {});
}

export interface WikiChangeItem {
  id: string;
  operation: 'create' | 'update' | 'archive';
  change_category: WikiChangeCategory;
  page_id?: string;
  page_slug: string;
  expected_version: number;
  before?: Record<string, any>;
  after: Record<string, any>;
  changed_fields: string[];
  evidence_chunk_ids: string[];
  evidence_excerpts?: Record<string, string>;
}

export interface WikiChangeSet {
  id: string;
  knowledge_base_id: string;
  knowledge_id?: string;
  status: string;
  review_level: 'L0' | 'L1';
  change_category: WikiChangeCategory;
  reasons: string[];
  model_id?: string;
  prompt_version?: string;
  review_comment?: string;
  created_at: string;
  reviewed_at?: string;
  items: WikiChangeItem[];
}

export type WikiChangeCategory = 'addition' | 'update' | 'conflict' | 'correction' | 'retirement' | 'merge_duplicate';
export type WikiConflictResolution = 'keep_existing' | 'adopt_candidate' | 'edit_candidate' | 'split_scope' | 'defer' | 'per_conflict';
export interface WikiConflictChoice {
  item_id: string;
  assessment_index: number;
  resolution: 'keep_existing' | 'adopt_candidate';
}

export function listWikiChangeSets(kbId: string, params?: {
  status?: string;
  review_level?: string;
  change_category?: WikiChangeCategory | '';
  limit?: number;
  offset?: number;
}) {
  const query = new URLSearchParams();
  Object.entries(params || {}).forEach(([key, value]) => {
    if (value !== undefined && value !== '') query.set(key, String(value));
  });
  const qs = query.toString();
  return get(`/api/v1/knowledgebase/${kbId}/wiki/change-sets${qs ? `?${qs}` : ''}`);
}

export function getWikiChangeSet(kbId: string, changeSetId: string) {
  return get(`/api/v1/knowledgebase/${kbId}/wiki/change-sets/${changeSetId}`);
}

export function reviewWikiChangeSet(
  kbId: string,
  changeSetId: string,
  data: {
    decision: 'approved' | 'rejected' | 'deferred';
    comment?: string;
    merge_into_slug?: string;
    item_overrides?: Record<string, Record<string, any>>;
    resolution?: WikiConflictResolution;
    retained_claim?: string;
    conflict_choices?: WikiConflictChoice[];
  },
) {
  return post(`/api/v1/knowledgebase/${kbId}/wiki/change-sets/${changeSetId}/review`, data);
}

export function batchReviewWikiChangeSets(
  kbId: string,
  data: { change_set_ids: string[]; decision: 'approved' | 'rejected'; comment?: string },
) {
  return post(`/api/v1/knowledgebase/${kbId}/wiki/change-sets/batch-review`, data);
}

export function reclassifyLegacyWiki(kbId: string, page = 1, pageSize = 200) {
  return post(`/api/v1/knowledgebase/${kbId}/wiki/governance/reclassify?page=${page}&page_size=${pageSize}`, {})
}
