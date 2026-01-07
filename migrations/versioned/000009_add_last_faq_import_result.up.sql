-- Add last_faq_import_result column to knowledge table
-- This field stores the latest FAQ import result for FAQ type knowledge

ALTER TABLE knowledge ADD COLUMN last_faq_import_result JSON DEFAULT NULL COMMENT 'Last FAQ import result (for FAQ type knowledge only)';