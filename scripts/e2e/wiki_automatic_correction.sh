#!/usr/bin/env bash
set -euo pipefail

API_BASE="${E2E_API_BASE:-http://localhost:8080/api/v1}"
EMAIL="${E2E_EMAIL:-}"
PASSWORD="${E2E_PASSWORD:-}"
EMBEDDING_MODEL_ID="${E2E_EMBEDDING_MODEL_ID:-}"
SUMMARY_MODEL_ID="${E2E_SUMMARY_MODEL_ID:-}"
TIMEOUT_SECONDS="${E2E_TIMEOUT_SECONDS:-900}"
CLEANUP="${E2E_CLEANUP:-0}"

if [[ -z "$EMAIL" || -z "$PASSWORD" || -z "$EMBEDDING_MODEL_ID" || -z "$SUMMARY_MODEL_ID" ]]; then
  echo "E2E_EMAIL, E2E_PASSWORD, E2E_EMBEDDING_MODEL_ID and E2E_SUMMARY_MODEL_ID are required" >&2
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
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -fsS -X "$method" -H "Authorization: Bearer $token" -H "Content-Type: application/json" --data "$body" "$API_BASE$path"
  else
    curl -fsS -X "$method" -H "Authorization: Bearer $token" "$API_BASE$path"
  fi
}

wait_for_knowledge() {
  local knowledge_id="$1" deadline=$(( $(date +%s) + TIMEOUT_SECONDS )) response status
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

wait_for_change_set() {
  local knowledge_id="$1" level="$2" category="$3" status="$4"
  local deadline=$(( $(date +%s) + TIMEOUT_SECONDS )) response id
  while (( $(date +%s) < deadline )); do
    response="$(request_json GET "/knowledgebase/$kb_id/wiki/change-sets?review_level=$level&change_category=$category&status=$status&limit=200")"
    id="$(jq -r --arg knowledge_id "$knowledge_id" '[.change_sets[]? | select(.knowledge_id == $knowledge_id)] | first | .id // empty' <<<"$response")"
    if [[ -n "$id" ]]; then
      printf '%s' "$id"
      return 0
    fi
    sleep 3
  done
  echo "timed out waiting for $level/$category/$status change set for $knowledge_id" >&2
  return 1
}

login_payload="$(jq -n --arg email "$EMAIL" --arg password "$PASSWORD" '{email:$email,password:$password}')"
login_response="$(curl -fsS -X POST -H 'Content-Type: application/json' --data "$login_payload" "$API_BASE/auth/login")"
token="$(jq -er '.token' <<<"$login_response")"

kb_payload="$(jq -n --arg name "E2E automatic correction $(date +%s)" --arg embedding "$EMBEDDING_MODEL_ID" --arg summary "$SUMMARY_MODEL_ID" '{name:$name,type:"document",description:"Disposable E2E fixture for authoritative automatic wiki correction",embedding_model_id:$embedding,summary_model_id:$summary,storage_provider_config:{provider:"local"},chunking_config:{strategy:"heading",chunk_size:512,chunk_overlap:50,separators:["\n\n","\n","。"]},question_generation_config:{enabled:false,question_count:0},indexing_strategy:{vector_enabled:true,keyword_enabled:true,wiki_enabled:true,graph_enabled:false},wiki_config:{synthesis_model_id:$summary,max_pages_per_ingest:0,extraction_granularity:"exhaustive"}}')"
kb_response="$(request_json POST /knowledge-bases "$kb_payload")"
kb_id="$(jq -er '.data.id' <<<"$kb_response")"

baseline_content='# 服务响应时限标准

## 适用范围
本正式制度适用于中国大陆客户服务团队已受理的普通服务请求。

## 正式规则
所有普通服务请求必须在3个工作日内完成响应。这是当前有效的正式制度。'
baseline_payload="$(jq -n --arg title '服务响应时限标准' --arg content "$baseline_content" '{title:$title,content:$content,status:"publish",channel:"e2e"}')"
baseline_response="$(request_json POST "/knowledge-bases/$kb_id/knowledge/manual" "$baseline_payload")"
baseline_knowledge_id="$(jq -er '.data.id' <<<"$baseline_response")"
baseline_status="$(wait_for_knowledge "$baseline_knowledge_id")"

baseline_deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
baseline_set_id=""
card_slug=""
baseline_ids=()
while (( $(date +%s) < baseline_deadline )); do
  baseline_list="$(request_json GET "/knowledgebase/$kb_id/wiki/change-sets?review_level=L1&change_category=addition&status=pending&limit=200")"
  baseline_ids=()
  while IFS= read -r id; do
    [[ -n "$id" ]] && baseline_ids+=("$id")
  done < <(jq -r --arg knowledge_id "$baseline_knowledge_id" '.change_sets[]? | select(.knowledge_id == $knowledge_id and any(.items[]?; .after.page_type == "card" and .after.knowledge_type == "rule")) | .id' <<<"$baseline_list")
  baseline_set_id="$(jq -r --arg knowledge_id "$baseline_knowledge_id" '[.change_sets[]? | select(.knowledge_id == $knowledge_id) | select(any(.items[]?; .after.page_type == "card" and .after.knowledge_type == "rule" and (((.after.summary // "") + " " + (.after.content // "")) as $text | (($text | contains("3个工作日")) or ($text | contains("3 business days"))))))] | first | .id // empty' <<<"$baseline_list")"
  if [[ -n "$baseline_set_id" && ${#baseline_ids[@]} -gt 0 ]]; then
    baseline_detail="$(request_json GET "/knowledgebase/$kb_id/wiki/change-sets/$baseline_set_id")"
    card_slug="$(jq -r '[.change_set.items[]? | select(.after.page_type == "card" and .after.knowledge_type == "rule" and (((.after.summary // "") + " " + (.after.content // "")) as $text | (($text | contains("3个工作日")) or ($text | contains("3 business days")))))] | first | .page_slug // empty' <<<"$baseline_detail")"
    [[ -n "$card_slug" ]] && break
  fi
  sleep 3
done
if [[ -z "$baseline_set_id" || -z "$card_slug" ]]; then
  echo "baseline produced no target response-time rule card" >&2
  exit 1
fi
for id in "${baseline_ids[@]}"; do
  request_json POST "/knowledgebase/$kb_id/wiki/change-sets/$id/review" '{"decision":"approved","comment":"E2E approves the baseline formal rule cards"}' >/dev/null
done

# Ensure source_updated_at is strictly newer even on filesystems/databases with
# coarse timestamp precision.
sleep 2
correction_content='# 服务响应时限标准

## 适用范围
本正式制度适用于中国大陆客户服务团队已受理的普通服务请求。

## 明确纠错
本文件是对旧制度的明确更正。服务响应时限现更正为5个工作日，原3个工作日的规定正式废止，以本版本为准。'
correction_payload="$(jq -n --arg title '服务响应时限标准-明确纠错版' --arg content "$correction_content" '{title:$title,content:$content,status:"publish",channel:"e2e"}')"
correction_response="$(request_json POST "/knowledge-bases/$kb_id/knowledge/manual" "$correction_payload")"
correction_knowledge_id="$(jq -er '.data.id' <<<"$correction_response")"
correction_status="$(wait_for_knowledge "$correction_knowledge_id")"

correction_set_id="$(wait_for_change_set "$correction_knowledge_id" L0 correction applied)"
correction_detail="$(request_json GET "/knowledgebase/$kb_id/wiki/change-sets/$correction_set_id")"

if ! jq -e --arg slug "$card_slug" '.change_set.review_level == "L0" and .change_set.change_category == "correction" and .change_set.status == "applied" and any(.change_set.reasons[]?; . == "authoritative_explicit_correction") and any(.change_set.items[]?; .page_slug == $slug and .operation == "update" and (.expected_version >= 1) and (.after.version > .expected_version)) and any(.reviews[]?; .reviewer_id == "system" and .resolution == "automatic_correction" and (.retained_claim | length > 0) and ((.discarded_claims // []) | length > 0))' <<<"$correction_detail" >/dev/null; then
  echo "automatic correction did not produce the expected L0 audit closure" >&2
  jq '.change_set, .reviews' <<<"$correction_detail" >&2
  exit 1
fi

encoded_slug="$(jq -rn --arg value "$card_slug" '$value | @uri')"
history_response="$(request_json GET "/knowledgebase/$kb_id/wiki/card-history?slug=$encoded_slug&limit=20")"
history_count="$(jq --arg id "$correction_set_id" '[.change_sets[]? | select(.id == $id)] | length' <<<"$history_response")"
if (( history_count == 0 )); then
  echo "automatic correction is missing from card history" >&2
  exit 1
fi

rollback_response="$(request_json POST "/knowledgebase/$kb_id/wiki/change-sets/$correction_set_id/rollback" '{}')"
rollback_level="$(jq -er '.change_set.review_level' <<<"$rollback_response")"
rollback_category="$(jq -er '.change_set.change_category' <<<"$rollback_response")"
rollback_status="$(jq -er '.change_set.status' <<<"$rollback_response")"
if [[ "$rollback_level/$rollback_category/$rollback_status" != "L1/correction/pending" ]]; then
  echo "rollback=$rollback_level/$rollback_category/$rollback_status, want L1/correction/pending" >&2
  exit 1
fi

finished_epoch="$(date +%s)"
jq -n --arg test wiki_automatic_correction_api --arg result PASS --arg started_at "$started_at" --arg kb_id "$kb_id" --arg baseline_knowledge_id "$baseline_knowledge_id" --arg correction_knowledge_id "$correction_knowledge_id" --arg baseline_status "$baseline_status" --arg correction_status "$correction_status" --arg card_slug "$card_slug" --arg baseline_set_id "$baseline_set_id" --arg correction_set_id "$correction_set_id" --arg rollback_level "$rollback_level" --arg rollback_category "$rollback_category" --arg rollback_status "$rollback_status" --argjson history_count "$history_count" --argjson duration_seconds "$(( finished_epoch - started_epoch ))" '{test:$test,result:$result,started_at:$started_at,duration_seconds:$duration_seconds,knowledge_base_id:$kb_id,documents:[{role:"baseline",knowledge_id:$baseline_knowledge_id,parse_status:$baseline_status},{role:"authoritative_correction",knowledge_id:$correction_knowledge_id,parse_status:$correction_status}],card_slug:$card_slug,change_sets:{baseline:$baseline_set_id,automatic_correction:$correction_set_id,history_count:$history_count},rollback_candidate:{review_level:$rollback_level,change_category:$rollback_category,status:$rollback_status}}'
