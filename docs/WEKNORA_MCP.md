# WeKnora Wiki/RAG MCP 服务

`cmd/weknora-mcp` 是一个独立的 MCP Streamable HTTP 适配器。它通过 WeKnora REST API 暴露 Wiki 问答、普通 RAG 问答、自然语言知识反馈和知识写入能力，并自带独立管理后台。

## 启动

先启动 WeKnora 后端和基础设施，然后准备一个拥有对应知识库权限的 WeKnora API Key：

```bash
export WEKNORA_API_URL=http://localhost:8080/api/v1
export WEKNORA_MCP_API_KEY=sk-...
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

### `ask_wiki`

调用 Wiki Agent。参数：`question`、`knowledge_base_id`，可选 `agent_id`。

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
      "knowledge_id": "...",
      "knowledge_base_id": "...",
      "parent_node_id": "...",
      "sub_node_ids": [],
      "knowledge_title": "...",
      "rank": 1,
      "score": 0.91,
      "match_type": "vector",
      "content_excerpt": "...",
      "content_hash": "sha256:..."
    }
  ]
}
```

`node_id` 是本次实际召回并交给回答链路的 Chunk/节点 ID。MCP 服务会按 `request_id` 保存本次召回快照，包含节点、知识、知识库、父子节点、排名、分数、匹配类型、内容摘要和内容哈希；即使节点内容后续被编辑，历史反馈仍可核对当时证据。

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

### `update_knowledge`

向指定知识库新增一条 Markdown 手工知识。参数：`knowledge_base_id`、`title`、`content`。

### `upload_knowledge_file`

上传 Base64 编码文件。支持 PDF、Word、Excel、PowerPoint、EPUB、MHTML、文本、Markdown、CSV、JSON、XML、HTML 和常见音频格式。

MCP 服务继承 `WEKNORA_MCP_API_KEY` 的权限；API Key 必须对目标知识库具有读取权限，写入工具还需要写入权限。Wiki Agent 需要在 WeKnora 中预先绑定 Wiki 知识库。

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
