#!/usr/bin/env bash
set -euo pipefail

API_BASE="${E2E_API_BASE:-http://localhost:8080/api/v1}"
DB_CONTAINER="${E2E_DB_CONTAINER:-WeKnora-postgres-dev}"
DB_USER="${E2E_DB_USER:-postgres}"
DB_NAME="${E2E_DB_NAME:-WeKnora}"
stamp="$(date +%s)-$$"
email="wiki-conflict-${stamp}@example.test"
username="wiki-conflict-${stamp}"
password="Conflict-${stamp}!"

command -v curl >/dev/null
command -v jq >/dev/null
command -v docker >/dev/null

register_payload="$(jq -n --arg username "$username" --arg email "$email" --arg password "$password" '{username:$username,email:$email,password:$password}')"
curl -fsS -X POST -H 'Content-Type: application/json' --data "$register_payload" "$API_BASE/auth/register" >/dev/null
login_payload="$(jq -n --arg email "$email" --arg password "$password" '{email:$email,password:$password}')"
login_response="$(curl -fsS -X POST -H 'Content-Type: application/json' --data "$login_payload" "$API_BASE/auth/login")"
token="$(jq -er '.token' <<<"$login_response")"
user_id="$(jq -er '.user.id' <<<"$login_response")"
tenant_id="$(jq -er '.active_tenant.id // .user.tenant_id' <<<"$login_response")"

uuid() { uuidgen | tr '[:upper:]' '[:lower:]'; }
kb_id="$(uuid)"
existing_id="$(uuid)"
set_id="$(uuid)"
item_id="$(uuid)"
candidate_slug="card/rule-quote-window-${stamp}"
existing_slug="entity/mai-jia-${stamp}"

existing_page="$(jq -n --arg id "$existing_id" --argjson tenant "$tenant_id" --arg kb "$kb_id" --arg slug "$existing_slug" '{id:$id,tenant_id:$tenant,knowledge_base_id:$kb,slug:$slug,title:"买家报价时长",page_type:"entity",status:"published",content:"手机商品报价窗口为30分钟；其他多品类商品报价窗口为2小时。",summary:"",review_status:"approved",version:3}')"
candidate_page="$(jq -n --argjson tenant "$tenant_id" --arg kb "$kb_id" --arg slug "$candidate_slug" --arg related "$existing_slug" '{tenant_id:$tenant,knowledge_base_id:$kb,slug:$slug,title:"报价窗口",page_type:"card",knowledge_type:"rule",maturity_status:"pending_review",answer_strength:"strong",review_status:"pending",status:"draft",content:"手机商品报价窗口为8分钟；其他多品类商品报价窗口为30分钟。",summary:"报价窗口规则",chunk_refs:["chunk-new"],version:1,page_metadata:{cross_page_assessments:[{related_slug:$related,relation:"conflicting",candidate_claim:"手机商品报价窗口为8分钟",existing_claim:"手机商品报价窗口为30分钟",reason:"手机报价时长不同",confidence:1,applicability_overlap:true},{related_slug:$related,relation:"conflicting",candidate_claim:"其他多品类商品报价窗口为30分钟",existing_claim:"其他多品类商品报价窗口为2小时",reason:"多品类报价时长不同",confidence:1,applicability_overlap:true}]}}')"

docker exec -i "$DB_CONTAINER" psql -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$DB_NAME" \
  -v kb_id="$kb_id" -v tenant_id="$tenant_id" -v user_id="$user_id" \
  -v existing_id="$existing_id" -v existing_slug="$existing_slug" -v existing_page="$existing_page" \
  -v set_id="$set_id" -v item_id="$item_id" -v candidate_slug="$candidate_slug" -v candidate_page="$candidate_page" <<'SQL' >/dev/null
INSERT INTO knowledge_bases (id, name, tenant_id, embedding_model_id, summary_model_id, creator_id, indexing_strategy, wiki_config)
VALUES (:'kb_id', 'Per-conflict E2E', :'tenant_id', '', '', :'user_id', '{"vector_enabled":false,"keyword_enabled":false,"wiki_enabled":true,"graph_enabled":false}'::jsonb, '{}'::jsonb);
INSERT INTO wiki_pages (id, tenant_id, knowledge_base_id, slug, title, page_type, status, content, review_status, version)
VALUES (:'existing_id', :'tenant_id', :'kb_id', :'existing_slug', '买家报价时长', 'entity', 'published', '手机商品报价窗口为30分钟；其他多品类商品报价窗口为2小时。', 'approved', 3);
INSERT INTO wiki_change_sets (id, tenant_id, knowledge_base_id, status, review_level, change_category, reasons, created_by)
VALUES (:'set_id', :'tenant_id', :'kb_id', 'pending', 'L1', 'conflict', '["cross_page_claim_conflict"]'::jsonb, :'user_id');
INSERT INTO wiki_change_items (id, change_set_id, operation, change_category, page_slug, after, changed_fields, evidence_chunk_ids)
VALUES (:'item_id', :'set_id', 'create', 'conflict', :'candidate_slug', :'candidate_page'::jsonb, '["content"]'::jsonb, '["chunk-new"]'::jsonb);
SQL

review_payload="$(jq -n --arg item "$item_id" '{decision:"approved",resolution:"per_conflict",comment:"E2E mixed conflict choices",conflict_choices:[{item_id:$item,assessment_index:0,resolution:"adopt_candidate"},{item_id:$item,assessment_index:1,resolution:"keep_existing"}]}')"
curl -fsS -X POST -H "Authorization: Bearer $token" -H 'Content-Type: application/json' --data "$review_payload" "$API_BASE/knowledgebase/$kb_id/wiki/change-sets/$set_id/review" >/dev/null
detail="$(curl -fsS -H "Authorization: Bearer $token" "$API_BASE/knowledgebase/$kb_id/wiki/change-sets/$set_id")"

if ! jq -e --arg candidate "$candidate_slug" --arg existing "$existing_slug" '
  .change_set.status == "applied"
  and any(.change_set.items[]?; .page_slug == $candidate and (.after.content | contains("手机商品报价窗口为8分钟")) and (.after.content | contains("其他多品类商品报价窗口为2小时")) and ((.after.content | contains("其他多品类商品报价窗口为30分钟")) | not))
  and any(.change_set.items[]?; .page_slug == $existing and .operation == "update" and (.after.content | contains("手机商品报价窗口为8分钟")) and (.after.content | contains("其他多品类商品报价窗口为2小时")))
  and any(.reviews[]?; .resolution == "per_conflict" and (.conflict_choices | length) == 2 and (.discarded_claims | length) == 2)
' <<<"$detail" >/dev/null; then
  echo "per-conflict API review did not apply the expected mixed result" >&2
  jq '.change_set, .reviews' <<<"$detail" >&2
  exit 1
fi

jq -n --arg test wiki_per_conflict_resolution_api --arg result PASS --arg kb_id "$kb_id" --arg change_set_id "$set_id" '{test:$test,result:$result,knowledge_base_id:$kb_id,change_set_id:$change_set_id,conflict_count:2,choices:["adopt_candidate","keep_existing"]}'
