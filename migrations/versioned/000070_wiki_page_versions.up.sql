CREATE TABLE IF NOT EXISTS wiki_page_versions (
  id VARCHAR(36) PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  knowledge_base_id VARCHAR(36) NOT NULL,
  page_id VARCHAR(36) NOT NULL,
  version INTEGER NOT NULL,
  state VARCHAR(24) NOT NULL,
  parent_version_id VARCHAR(36) NOT NULL DEFAULT '',
  snapshot JSONB NOT NULL,
  change_summary TEXT NOT NULL DEFAULT '',
  created_by VARCHAR(255) NOT NULL DEFAULT '',
  published_by VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  published_at TIMESTAMPTZ,
  archived_at TIMESTAMPTZ,
  CONSTRAINT uq_wiki_page_versions_page_version UNIQUE (page_id, version),
  CONSTRAINT chk_wiki_page_versions_state CHECK (state IN ('draft', 'published', 'history', 'archived'))
);
CREATE INDEX IF NOT EXISTS idx_wiki_page_versions_list ON wiki_page_versions (knowledge_base_id, page_id, state, version DESC);
CREATE INDEX IF NOT EXISTS idx_wiki_page_versions_parent ON wiki_page_versions (parent_version_id);
