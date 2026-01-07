-- Migration: Create "未分类" tag for each knowledge base that has untagged FAQ entries
-- and update those entries to reference the new tag

DO $$
DECLARE
    kb_record RECORD;
    new_tag_id VARCHAR(36);
    updated_chunks INT;
    updated_embeddings INT;
BEGIN
    -- Find all knowledge bases that have FAQ chunks with empty or NULL tag_id
    FOR kb_record IN 
        SELECT DISTINCT c.tenant_id, c.knowledge_base_id 
        FROM chunks c
        WHERE c.chunk_type = 'faq' 
        AND (c.tag_id = '' OR c.tag_id IS NULL)
    LOOP
        -- Check if "未分类" tag already exists for this knowledge base
        SELECT id INTO new_tag_id
        FROM knowledge_tags
        WHERE tenant_id = kb_record.tenant_id 
        AND knowledge_base_id = kb_record.knowledge_base_id 
        AND name = '未分类'
        LIMIT 1;

        -- If not exists, create the tag
        IF new_tag_id IS NULL THEN
            new_tag_id := gen_random_uuid()::VARCHAR(36);
            INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name, color, sort_order, created_at, updated_at)
            VALUES (new_tag_id, kb_record.tenant_id, kb_record.knowledge_base_id, '未分类', '', 0, NOW(), NOW());
            RAISE NOTICE '[Migration 000008] Created "未分类" tag (id: %) for tenant_id: %, kb_id: %', 
                new_tag_id, kb_record.tenant_id, kb_record.knowledge_base_id;
        ELSE
            RAISE NOTICE '[Migration 000008] "未分类" tag already exists (id: %) for tenant_id: %, kb_id: %', 
                new_tag_id, kb_record.tenant_id, kb_record.knowledge_base_id;
        END IF;

        -- Update chunks with empty tag_id to use the new tag
        UPDATE chunks 
        SET tag_id = new_tag_id, updated_at = NOW()
        WHERE tenant_id = kb_record.tenant_id 
        AND knowledge_base_id = kb_record.knowledge_base_id 
        AND chunk_type = 'faq'
        AND (tag_id = '' OR tag_id IS NULL);
        
        GET DIAGNOSTICS updated_chunks = ROW_COUNT;
        RAISE NOTICE '[Migration 000008] Updated % chunks for tenant_id: %, kb_id: %', 
            updated_chunks, kb_record.tenant_id, kb_record.knowledge_base_id;

        -- Update embeddings with empty tag_id (if embeddings table exists and has tag_id column)
        IF EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'embeddings' AND column_name = 'tag_id'
        ) THEN
            UPDATE embeddings 
            SET tag_id = new_tag_id
            WHERE knowledge_base_id = kb_record.knowledge_base_id 
            AND (tag_id = '' OR tag_id IS NULL)
            AND chunk_id IN (
                SELECT id FROM chunks 
                WHERE tenant_id = kb_record.tenant_id 
                AND knowledge_base_id = kb_record.knowledge_base_id 
                AND chunk_type = 'faq'
            );
            
            GET DIAGNOSTICS updated_embeddings = ROW_COUNT;
            RAISE NOTICE '[Migration 000008] Updated % embeddings for kb_id: %', 
                updated_embeddings, kb_record.knowledge_base_id;
        END IF;
    END LOOP;

    RAISE NOTICE '[Migration 000008] Completed migration of untagged FAQ entries';
END $$;
