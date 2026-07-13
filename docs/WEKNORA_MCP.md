# WeKnora Wiki/RAG MCP 服务

`cmd/weknora-mcp` 是一个独立的 MCP Streamable HTTP 适配器。它通过 WeKnora REST API 暴露 Wiki 问答、普通 RAG 问答和知识写入能力，并自带独立管理后台。

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

后台使用 `WEKNORA_MCP_ADMIN_TOKEN` 登录，可新增成员、生成或轮换成员令牌、分别控制问答和知识写入权限、立即禁用成员，并查看按成员记录的工具、状态、问题/文件名、知识库、耗时、来源 IP 和 Request ID。数据默认保存在 `data/weknora-mcp-admin.db`，可通过 `WEKNORA_MCP_ADMIN_DB` 修改。

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

### `ask_rag`

调用普通知识库 RAG 问答。参数：`question`、`knowledge_base_id`。

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
