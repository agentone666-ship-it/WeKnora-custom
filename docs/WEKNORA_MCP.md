# WeKnora Wiki/RAG MCP 服务

`cmd/weknora-mcp` 是一个独立的 MCP Streamable HTTP 适配器。它通过 WeKnora REST API 暴露 Wiki 问答、普通 RAG 问答、自然语言知识反馈和知识写入能力，并自带独立管理后台。

## 启动

先启动 WeKnora 后端和基础设施，然后准备一个拥有对应知识库权限的 WeKnora API Key：

```bash
export WEKNORA_API_URL=http://localhost:8080/api/v1
export WEKNORA_MCP_API_KEY=sk-...
# 租户启用 API Principal 外部用户模式时设置
export WEKNORA_MCP_EXTERNAL_USER_ID=mcp-service
export WEKNORA_WIKI_AGENT_ID=<Wiki Agent ID>
export WEKNORA_MCP_AUTH_TOKEN=<首次接入令牌>
export WEKNORA_MCP_ADMIN_TOKEN=<独立管理后台密钥>
make mcp-server
```

MCP 地址：

```text
http://localhost:8787/mcp
```

独立管理后台：

```text
http://localhost:8787/admin
```

后台使用 `WEKNORA_MCP_ADMIN_TOKEN` 登录，可新增成员、生成或轮换成员令牌、分别控制问答和知识写入权限、立即禁用成员，并查看按成员记录的工具、状态、问题/文件名、知识库、耗时、来源 IP 和 Request ID。后台还提供“知识反馈”页面，可查看反馈正文、类型、关联问答、召回节点、目标错误节点、历史内容摘要与哈希，以及溯源校验状态。数据默认保存在 `data/weknora-mcp-admin.db`，可通过 `WEKNORA_MCP_ADMIN_DB` 修改。

首次升级时，旧的 `WEKNORA_MCP_AUTH_TOKEN` 和 `WEKNORA_MCP_WRITE_TOKENS` 会自动迁移成后台成员，原有设备无需修改。未设置独立管理密钥时，为兼容旧部署，管理后台暂时接受旧的只读令牌；正式对外提供服务前应设置单独的 `WEKNORA_MCP_ADMIN_TOKEN`。

也可以使用：

```bash
go run ./cmd/weknora-mcp \
  --listen :8787 \
  --api-url http://localhost:8080/api/v1 \
  --api-key sk-... \
  --wiki-agent-id <Wiki Agent ID>
```

## 工具

服务端名称为 `weknora`，当前暴露 17 个工具。原有 5 个工具保持兼容，新增 1 个知识反馈工具，同时提供与 ClawHub 官方 `lyingbug/skills/weknora` v1.0.1 工作流对应的 11 个工具。

### 问答与反馈溯源

除了兼容旧客户端的纯文本回答，工具会通过 MCP `structuredContent` 返回：

```json
{
  "answer": "...",
  "request_id": "...",
  "session_id": "...",
  "knowledge_base_id": "...",
  "recalled_nodes": [
    {
      "node_id": "...",
      "source_type": "wiki_page",
      "wiki_slug": "entity/example",
      "knowledge_id": "...",
      "knowledge_ids": ["..."],
      "knowledge_base_id": "...",
      "parent_node_id": "...",
      "sub_node_ids": [],
      "knowledge_title": "...",
      "rank": 1,
      "score": 0.91,
      "match_type": "entity",
      "content_excerpt": "...",
      "content_hash": "sha256:..."
    }
  ]
}
```

`ask_wiki` 会在 Agent 完成后读取已持久化的最终回答，因此不会把 `thinking` / `tool_call` / `tool_result` 等内部进度日志混入用户答案。对 Agent 实际读取或在答案中引用的 Wiki 页，`node_id` 是 Wiki Page UUID，`wiki_slug` 是可读标识，`knowledge_ids` 和 `sub_node_ids` 分别保存源文档与底层 Chunk ID。

`ask_rag` 的 `node_id` 是本次实际召回并交给回答链路的 Chunk/节点 ID。MCP 服务会按 `request_id` 保存两类问答的召回快照，包含节点、知识、知识库、父子节点、排名、分数、匹配类型、内容摘要和内容哈希；即使内容后续被编辑，历史反馈仍可核对当时证据。

### `ask_rag`

调用普通知识库 RAG 问答。参数：`question`、`knowledge_base_id`。返回结构和召回快照规则与 `ask_wiki` 相同。

### `submit_knowledge_feedback`

收集用户对知识回答或引用证据的自然语言反馈。Agent 在用户表示“回答错误、过时、不完整、不相关、引用错误、缺少知识”，或用户主动给出修正/改进建议时，应调用本工具。这个工具只记录反馈，**不会直接修改正式知识**，只读成员令牌即可调用。

只有 `feedback_text` 必填，其余上下文都可缺省：

- `original_question`
- `answer_excerpt`
- `suggested_correction`
- `feedback_type`：`incorrect`、`outdated`、`incomplete`、`irrelevant`、`wrong_reference`、`missing_knowledge`、`suggestion`、`other`
- `related_request_id`
- `knowledge_base_id`
- `recalled_node_ids`：该回答实际召回的全部节点，应直接复用问答结果，不要猜测
- `target_node_ids`：用户明确指出有问题的节点子集；不明确时必须省略，不要猜测

推荐调用方式：

```json
{
  "feedback_text": "第二条依据已经过时，现行规则是……",
  "feedback_type": "outdated",
  "original_question": "……",
  "related_request_id": "ask_wiki 返回的 request_id",
  "recalled_node_ids": ["node-1", "node-2"],
  "target_node_ids": ["node-2"],
  "suggested_correction": "现行规则是……"
}
```

溯源规则：

1. 有 `related_request_id` 时，服务端按保存的召回快照验证节点。
2. 只传 `related_request_id` 而未传 `recalled_node_ids` 时，服务端自动补全本次召回节点。
3. `target_node_ids` 必须是 `recalled_node_ids` 的子集。
4. 节点或知识库不匹配时，反馈仍会保存，但标记为 `trace_mismatch`，不会静默绑定到错误节点。
5. 没有可用上下文时仍可只提交 `feedback_text`，状态为 `untraced`。
### 官方 Skill 兼容工具

| MCP 工具 | 对应 WeKnora REST 工作流 | 权限 |
| --- | --- | --- |
| `list_knowledge_bases` | `GET /knowledge-bases` | reader |
| `get_knowledge_base` | `GET /knowledge-bases/:id` | reader |
| `create_manual_knowledge` | `POST /knowledge-bases/:id/knowledge/manual` | writer |
| `import_knowledge_url` | `POST /knowledge-bases/:id/knowledge/url` | writer |
| `get_knowledge` | `GET /knowledge/:id` | reader |
| `list_knowledge` | `GET /knowledge-bases/:id/knowledge` | reader |
| `update_manual_knowledge` | `PUT /knowledge/manual/:id` | writer |
| `delete_knowledge` | `DELETE /knowledge/:id` | writer |
| `reparse_knowledge` | `POST /knowledge/:id/reparse` | writer |
| `hybrid_search` | `POST /knowledge-bases/:id/hybrid-search` | reader |
| `search_knowledge` | `POST /knowledge-search` | reader |

兼容层会为每次后端 API 请求同时设置 `Authorization`、`X-API-Key`、`X-Request-ID`，配置了 `WEKNORA_MCP_EXTERNAL_USER_ID` 时还会设置 `X-External-User-ID`。文件上传继续使用 multipart/form-data；`upload_knowledge_file` 由 MCP 客户端传入 Base64 内容，再由服务端转换为 multipart 请求。

官方 Skill 文档中的单数 `tag_id` 与后端的复数 `tag_ids` 均可使用，MCP 会去重后统一传给 WeKnora。`list_knowledge` 支持 `page`、`page_size`、`tag_id`/`tag_ids`、`keyword`、`file_type`、`parse_status` 和 `source`。`hybrid_search` 支持向量/关键词阈值、`match_count`、知识条目范围、标签范围和禁用单路检索等参数。

### 保留的兼容工具

| MCP 工具 | 说明 | 权限 |
| --- | --- | --- |
| `search_knowledge_bases` | 按名称/描述搜索当前 API Key 可访问的知识库，并标记默认知识库 | reader |
| `ask_wiki` | 通过 Wiki Agent 问答 | reader |
| `ask_rag` | 通过普通 RAG 流程问答 | reader |
| `update_knowledge` | 新增 Markdown 手工知识（旧工具名） | writer |
| `upload_knowledge_file` | 上传 Base64 编码文件，支持文档、表格、演示文稿、文本、网页和常见音频格式 | writer |

### 权限与知识库范围

- reader 成员可以浏览知识库、查看知识详情和执行检索/问答。
- writer 成员除 reader 能力外，还可以新增、导入、更新、删除、重新解析和上传知识。
- MCP 服务继承 `WEKNORA_MCP_API_KEY` 对 WeKnora 的实际权限；成员具有 writer 权限并不代表后端 API Key 一定拥有目标知识库写权限。
- 设置 `WEKNORA_MCP_KB_ID` 后，所有带知识库范围的工具都会固定到该知识库，并拒绝显式访问其他知识库。
- Wiki Agent 需要在 WeKnora 中预先绑定 Wiki 知识库。

## 客户端配置示例

```json
{
  "mcpServers": {
    "weknora": {
      "url": "http://localhost:8787/mcp",
      "headers": {
        "Authorization": "Bearer <成员令牌>"
      }
    }
  }
}
```
