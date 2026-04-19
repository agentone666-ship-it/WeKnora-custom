CREATE TABLE IF NOT EXISTS wiki_page_issues (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    slug VARCHAR(255) NOT NULL,
    issue_type VARCHAR(50) NOT NULL,
    description TEXT NOT NULL,
    suspected_knowledge_ids JSONB,
    status VARCHAR(20) DEFAULT 'pending' NOT NULL,
    reported_by VARCHAR(100) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_wiki_page_issues_tenant_id ON wiki_page_issues(tenant_id);
CREATE INDEX IF NOT EXISTS idx_wiki_page_issues_knowledge_base_id ON wiki_page_issues(knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_wiki_page_issues_slug ON wiki_page_issues(slug);
CREATE INDEX IF NOT EXISTS idx_wiki_page_issues_status ON wiki_page_issues(status);
