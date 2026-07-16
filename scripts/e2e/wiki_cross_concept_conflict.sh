#!/usr/bin/env bash
set -euo pipefail

API_BASE="${E2E_API_BASE:-http://localhost:8080/api/v1}"
EMAIL="${E2E_EMAIL:-}"
PASSWORD="${E2E_PASSWORD:-}"
EMBEDDING_MODEL_ID="${E2E_EMBEDDING_MODEL_ID:-}"
SUMMARY_MODEL_ID="${E2E_SUMMARY_MODEL_ID:-}"
TIMEOUT_SECONDS="${E2E_TIMEOUT_SECONDS:-900}"
CLEANUP="${E2E_CLEANUP:-0}"

if [[ -z "$EMAIL" || -z "$PASSWORD" ]]; then
  echo "E2E_EMAIL and E2E_PASSWORD are required" >&2
  exit 2
fi
if [[ -z "$EMBEDDING_MODEL_ID" || -z "$SUMMARY_MODEL_ID" ]]; then
  echo "E2E_EMBEDDING_MODEL_ID and E2E_SUMMARY_MODEL_ID are required" >&2
  exit 2
fi
command -v curl >/dev/null
command -v jq >/dev/null

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
started_epoch="$(date +%s)"
kb_id=""

cleanup_fixture() {
  if [[ "$CLEANUP" == "1" && -n "${token:-}" && -n "$kb_id" ]]; then
    curl -fsS -X DELETE -H "Authorization: Bearer $token" "$API_BASE/knowledge-bases/$kb_id" >/dev/null || true
  fi
}
trap cleanup_fixture EXIT

request_json() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $token" \
      -H "Content-Type: application/json" \
      --data "$body" \
      "$API_BASE$path"
  else
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $token" \
      "$API_BASE$path"
  fi
}

login_payload="$(jq -n --arg email "$EMAIL" --arg password "$PASSWORD" '{email:$email,password:$password}')"
login_response="$(curl -fsS -X POST -H 'Content-Type: application/json' --data "$login_payload" "$API_BASE/auth/login")"
token="$(jq -er '.token' <<<"$login_response")"

kb_payload="$(jq -n \
  --arg name "E2E cross-concept conflict $(date +%s)" \
  --arg embedding "$EMBEDDING_MODEL_ID" \
  --arg summary "$SUMMARY_MODEL_ID" \
  '{
    name:$name,
    type:"document",
    description:"Disposable end-to-end fixture for cross-concept wiki conflict detection",
    embedding_model_id:$embedding,
    summary_model_id:$summary,
    storage_provider_config:{provider:"local"},
    chunking_config:{strategy:"heading",chunk_size:512,chunk_overlap:50,separators:["\n\n","\n","。"]},
    question_generation_config:{enabled:false,question_count:0},
    indexing_strategy:{vector_enabled:true,keyword_enabled:true,wiki_enabled:true,graph_enabled:false},
    wiki_config:{synthesis_model_id:$summary,max_pages_per_ingest:0,extraction_granularity:"exhaustive"}
  }')"
kb_response="$(request_json POST /knowledge-bases "$kb_payload")"
kb_id="$(jq -er '.data.id' <<<"$kb_response")"

wait_for_knowledge() {
  local knowledge_id="$1"
  local deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
  local response status
  while (( $(date +%s) < deadline )); do
    response="$(request_json GET "/knowledge/$knowledge_id")"
    status="$(jq -r '.data.parse_status // .parse_status // empty' <<<"$response")"
    case "$status" in
      completed) printf '%s' "$status"; return 0 ;;
      failed|cancelled) echo "knowledge $knowledge_id ended with status=$status" >&2; return 1 ;;
    esac
    sleep 3
  done
  echo "timed out waiting for knowledge $knowledge_id" >&2
  return 1
}

wait_for_cross_page_change_set() {
  local deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
  local response id
  while (( $(date +%s) < deadline )); do
    response="$(request_json GET "/knowledgebase/$kb_id/wiki/change-sets?review_level=L1&change_category=conflict&limit=100")"
    id="$(jq -r '[.change_sets[] | select(.change_category == "conflict" and any(.reasons[]?; startswith("cross_page_")))] | first | .id // empty' <<<"$response")"
    if [[ -n "$id" ]]; then
      printf '%s' "$id"
      return 0
    fi
    sleep 3
  done
  echo "timed out waiting for a cross-page L1 conflict change set" >&2
  return 1
}

first_content='# 财务退款结算时限制度（2026版）

## 适用范围
本制度适用于中国大陆电商平台中已经审批通过的消费者售后退款，不适用于海外业务或尚未审批的申请。

## 正式规则
所有退款必须自退款审批通过之日起 **3 个工作日内** 完成到账。财务结算、支付渠道处理和客服通知均不得把该时限延长为 5 个工作日。

这是当前有效的正式制度。客服、财务和售后团队都必须执行 3 个工作日到账标准。'

second_content='# 客服售后响应与到账规范（2026版）

## 适用范围
本规范同样适用于中国大陆电商平台中已经审批通过的消费者售后退款，不适用于海外业务或尚未审批的申请。

## 正式规则
所有退款必须自退款审批通过之日起 **5 个工作日内** 完成到账。客服应向消费者承诺 5 个工作日到账，3 个工作日不是现行要求。

这是当前有效的正式规范。客服、财务和售后团队都必须执行 5 个工作日到账标准。'

first_payload="$(jq -n --arg title '财务退款结算时限制度' --arg content "$first_content" '{title:$title,content:$content,status:"publish",channel:"e2e"}')"
first_response="$(request_json POST "/knowledge-bases/$kb_id/knowledge/manual" "$first_payload")"
first_knowledge_id="$(jq -er '.data.id' <<<"$first_response")"
first_status="$(wait_for_knowledge "$first_knowledge_id")"

# Wait until the first document has materialized at least one entity/concept page.
first_pages_deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
while (( $(date +%s) < first_pages_deadline )); do
  pages_response="$(request_json GET "/knowledgebase/$kb_id/wiki/pages?page=1&page_size=100&include_unreviewed=true")"
  first_related_page_count="$(jq '[.pages[]? | select(.page_type == "entity" or .page_type == "concept")] | length' <<<"$pages_response")"
  if (( first_related_page_count > 0 )); then
    break
  fi
  sleep 3
done
if (( ${first_related_page_count:-0} == 0 )); then
  echo "first document produced no entity/concept pages" >&2
  exit 1
fi

second_payload="$(jq -n --arg title '客服售后响应与到账规范' --arg content "$second_content" '{title:$title,content:$content,status:"publish",channel:"e2e"}')"
second_response="$(request_json POST "/knowledge-bases/$kb_id/knowledge/manual" "$second_payload")"
second_knowledge_id="$(jq -er '.data.id' <<<"$second_response")"
second_status="$(wait_for_knowledge "$second_knowledge_id")"

change_set_id="$(wait_for_cross_page_change_set)"
detail_response="$(request_json GET "/knowledgebase/$kb_id/wiki/change-sets/$change_set_id")"
review_level="$(jq -er '.change_set.review_level' <<<"$detail_response")"
reasons="$(jq -c '.change_set.reasons' <<<"$detail_response")"
assessment_count="$(jq '[.change_set.items[]?.after.page_metadata.cross_page_assessments[]?] | length' <<<"$detail_response")"
related_evidence_count="$(jq '[.change_set.items[]?.after.page_metadata.related_page_evidence // {} | keys[]] | length' <<<"$detail_response")"

change_category="$(jq -er '.change_set.change_category' <<<"$detail_response")"
if [[ "$review_level" != "L1" || "$change_category" != "conflict" ]]; then
  echo "change set level/category=$review_level/$change_category, want L1/conflict" >&2
  exit 1
fi

retained_claim='中国大陆电商平台已审批消费者退款，以新制度的5个工作日到账说法为当前有效版本。'
review_payload="$(jq -n --arg retained "$retained_claim" '{decision:"approved",resolution:"adopt_candidate",retained_claim:$retained,comment:"E2E adopts the new rule and retires conflicting old versions"}')"
request_json POST "/knowledgebase/$kb_id/wiki/change-sets/$change_set_id/review" "$review_payload" >/dev/null

applied_detail="$(request_json GET "/knowledgebase/$kb_id/wiki/change-sets/$change_set_id")"
applied_status="$(jq -er '.change_set.status' <<<"$applied_detail")"
retired_page_count="$(jq '[.change_set.items[]? | select(.operation == "archive" and .change_category == "retirement")] | length' <<<"$applied_detail")"
audit_count="$(jq --arg retained "$retained_claim" '[.reviews[]? | select(.resolution == "adopt_candidate" and .retained_claim == $retained and ((.discarded_claims // []) | length > 0))] | length' <<<"$applied_detail")"
if [[ "$applied_status" != "applied" || "$retired_page_count" -eq 0 || "$audit_count" -eq 0 ]]; then
  echo "approved conflict did not apply retirement/audit closure" >&2
  exit 1
fi

retired_slug="$(jq -r '[.change_set.items[]? | select(.operation == "archive")] | first | .page_slug' <<<"$applied_detail")"
ordinary_pages="$(request_json GET "/knowledgebase/$kb_id/wiki/pages?page=1&page_size=200")"
if jq -e --arg slug "$retired_slug" 'any(.pages[]?; .slug == $slug)' <<<"$ordinary_pages" >/dev/null; then
  echo "retired page is still visible to ordinary wiki retrieval" >&2
  exit 1
fi
encoded_slug="$(jq -rn --arg value "$retired_slug" '$value | @uri')"
history_response="$(request_json GET "/knowledgebase/$kb_id/wiki/card-history?slug=$encoded_slug&limit=20")"
history_count="$(jq --arg id "$change_set_id" '[.change_sets[]? | select(.id == $id)] | length' <<<"$history_response")"
if (( history_count == 0 )); then
  echo "retired page has no traceable change-set history" >&2
  exit 1
fi
if ! jq -e 'any(.change_set.reasons[]?; startswith("cross_page_"))' <<<"$detail_response" >/dev/null; then
  echo "change set has no cross_page reason" >&2
  exit 1
fi
if (( assessment_count == 0 || related_evidence_count == 0 )); then
  echo "change set is missing cross-page assessments or evidence" >&2
  exit 1
fi

finished_epoch="$(date +%s)"
jq -n \
  --arg test "wiki_cross_concept_conflict_api" \
  --arg result "PASS" \
  --arg started_at "$started_at" \
  --arg api_base "$API_BASE" \
  --arg kb_id "$kb_id" \
  --arg first_knowledge_id "$first_knowledge_id" \
  --arg second_knowledge_id "$second_knowledge_id" \
  --arg first_status "$first_status" \
  --arg second_status "$second_status" \
  --arg change_set_id "$change_set_id" \
  --arg review_level "$review_level" \
  --arg change_category "$change_category" \
  --arg applied_status "$applied_status" \
  --argjson reasons "$reasons" \
  --argjson first_related_page_count "$first_related_page_count" \
  --argjson assessment_count "$assessment_count" \
  --argjson related_evidence_count "$related_evidence_count" \
  --argjson retired_page_count "$retired_page_count" \
  --argjson audit_count "$audit_count" \
  --argjson history_count "$history_count" \
  --argjson duration_seconds "$(( finished_epoch - started_epoch ))" \
  '{
    test:$test,result:$result,started_at:$started_at,duration_seconds:$duration_seconds,
    api_base:$api_base,knowledge_base_id:$kb_id,
    documents:[
      {role:"baseline",knowledge_id:$first_knowledge_id,parse_status:$first_status},
      {role:"conflicting_update",knowledge_id:$second_knowledge_id,parse_status:$second_status}
    ],
    first_document_related_pages:$first_related_page_count,
    change_set:{id:$change_set_id,review_level:$review_level,change_category:$change_category,reasons:$reasons,
      assessment_count:$assessment_count,related_evidence_count:$related_evidence_count,status:$applied_status,
      retired_page_count:$retired_page_count,audit_count:$audit_count,history_count:$history_count}
  }'
