-- Scenario-driven wiki cards and human review workflow.
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS knowledge_type VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS maturity_status VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS answer_strength VARCHAR(16) NOT NULL DEFAULT 'unknown';
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS review_status VARCHAR(32) NOT NULL DEFAULT 'approved';
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS business_line VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS scenario_ids JSONB NOT NULL DEFAULT '[]'::JSONB;
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS audience_roles JSONB NOT NULL DEFAULT '[]'::JSONB;
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS affected_metrics JSONB NOT NULL DEFAULT '[]'::JSONB;
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS applicability JSONB NOT NULL DEFAULT '{}'::JSONB;
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS prohibited_claims JSONB NOT NULL DEFAULT '[]'::JSONB;
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS reviewed_by VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS effective_from TIMESTAMPTZ;
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS effective_to TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_wiki_pages_card_filter
  ON wiki_pages (knowledge_base_id, page_type, review_status, maturity_status, knowledge_type);
CREATE INDEX IF NOT EXISTS idx_wiki_pages_business_line
  ON wiki_pages (knowledge_base_id, business_line);

CREATE TABLE IF NOT EXISTS wiki_packages (
  id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, knowledge_base_id VARCHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL, package_type VARCHAR(32) NOT NULL, business_line VARCHAR(128) NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '', loading_policy VARCHAR(32) NOT NULL DEFAULT 'on_demand',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wiki_packages_kb ON wiki_packages (knowledge_base_id, package_type, business_line);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wiki_packages_unique_name ON wiki_packages (knowledge_base_id, package_type, business_line, name);

CREATE TABLE IF NOT EXISTS wiki_package_pages (
  package_id VARCHAR(36) NOT NULL, page_id VARCHAR(36) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (package_id, page_id)
);

CREATE TABLE IF NOT EXISTS wiki_scenarios (
  id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, knowledge_base_id VARCHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL, business_line VARCHAR(128) NOT NULL DEFAULT '',
  target_roles JSONB NOT NULL DEFAULT '[]'::JSONB, affected_metrics JSONB NOT NULL DEFAULT '[]'::JSONB,
  priority VARCHAR(16) NOT NULL DEFAULT 'medium', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wiki_scenarios_kb ON wiki_scenarios (knowledge_base_id, business_line);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wiki_scenarios_unique_name ON wiki_scenarios (knowledge_base_id, business_line, name);

CREATE TABLE IF NOT EXISTS wiki_change_sets (
  id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, knowledge_base_id VARCHAR(36) NOT NULL,
  knowledge_id VARCHAR(36) NOT NULL DEFAULT '', status VARCHAR(32) NOT NULL DEFAULT 'pending',
  review_level VARCHAR(8) NOT NULL, reasons JSONB NOT NULL DEFAULT '[]'::JSONB,
  model_id VARCHAR(64) NOT NULL DEFAULT '', prompt_version VARCHAR(32) NOT NULL DEFAULT '',
  candidate_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
  created_by VARCHAR(36) NOT NULL DEFAULT '', reviewed_by VARCHAR(36) NOT NULL DEFAULT '',
  review_comment TEXT NOT NULL DEFAULT '', reviewed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wiki_change_sets_queue ON wiki_change_sets (knowledge_base_id, status, review_level, created_at);
CREATE INDEX IF NOT EXISTS idx_wiki_change_sets_fingerprint ON wiki_change_sets (knowledge_base_id, candidate_fingerprint, created_at);

CREATE TABLE IF NOT EXISTS wiki_change_items (
  id VARCHAR(36) PRIMARY KEY, change_set_id VARCHAR(36) NOT NULL, operation VARCHAR(16) NOT NULL,
  page_id VARCHAR(36) NOT NULL DEFAULT '', page_slug VARCHAR(255) NOT NULL, expected_version INT NOT NULL DEFAULT 0,
  before JSONB NOT NULL DEFAULT '{}'::JSONB, after JSONB NOT NULL DEFAULT '{}'::JSONB,
  changed_fields JSONB NOT NULL DEFAULT '[]'::JSONB, evidence_chunk_ids JSONB NOT NULL DEFAULT '[]'::JSONB,
  evidence_excerpts JSONB NOT NULL DEFAULT '{}'::JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wiki_change_items_set ON wiki_change_items (change_set_id);
CREATE INDEX IF NOT EXISTS idx_wiki_change_items_page ON wiki_change_items (page_slug);

CREATE TABLE IF NOT EXISTS wiki_reviews (
  id VARCHAR(36) PRIMARY KEY, change_set_id VARCHAR(36) NOT NULL, reviewer_id VARCHAR(36) NOT NULL,
  decision VARCHAR(16) NOT NULL, comment TEXT NOT NULL DEFAULT '', item_overrides JSONB NOT NULL DEFAULT '{}'::JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wiki_reviews_set ON wiki_reviews (change_set_id, created_at);

CREATE TABLE IF NOT EXISTS wiki_governance_metric_events (
  id VARCHAR(36) PRIMARY KEY, knowledge_base_id VARCHAR(36) NOT NULL,
  metric_name VARCHAR(64) NOT NULL, value BIGINT NOT NULL DEFAULT 1,
  metadata JSONB NOT NULL DEFAULT '{}'::JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wiki_governance_metric_events ON wiki_governance_metric_events (knowledge_base_id, metric_name, created_at);
