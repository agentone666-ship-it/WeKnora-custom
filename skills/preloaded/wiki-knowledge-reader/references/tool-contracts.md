# Wiki 读取工具契约

## 鉴权契约

这些工具必须经 WeKnora MCP/API 调用，调用者身份由主服务统一判定：

- 前台 JWT：使用当前租户的 `owner`、`admin`、`contributor` 或 `viewer` 角色；
- 租户 API Key：Wiki 三个读取工具要求 `retrieve` capability，且只能访问 API Key 白名单内的知识库；
- `tools/list` 未返回某个工具时，视为当前身份无权使用；
- HTTP `401` 表示凭证失效，`403` 表示 capability、角色或知识库范围不足；
- 不得改用数据库查询、服务器文件读取等方式规避 API 权限检查。

## 目录

- [wiki_search](#wiki_search)
- [wiki_read_page](#wiki_read_page)
- [wiki_read_source_doc](#wiki_read_source_doc)
- [典型调用链](#典型调用链)
- [证据记录格式](#证据记录格式)

## `wiki_search`

用途：按标题、摘要和 Wiki 内容查找候选页面。

输入：

```json
{
  "queries": ["批量.*回收", "成色", "竞拍|寄售"],
  "limit": 10,
  "knowledge_base_id": "可选的知识库 ID"
}
```

- `queries`：必填字符串数组；每项是大小写不敏感的 PostgreSQL 正则表达式。
- `limit`：每个查询最多返回的结果数，默认 10。
- `knowledge_base_id`：可选。多知识库场景应传入，避免同名节点混淆。

把搜索结果当作候选列表。至少读取最相关页面后再回答。

安全构造查询：

- 普通专名可直接使用，例如 `以旧换新`。
- 同义词可用 `竞拍|拍卖`。
- 用户输入包含正则字符时进行转义，例如把 `A+B` 写成 `A\+B`。
- 避免单独使用 `.*`、`.+` 或过宽单字查询。

## `wiki_read_page`

用途：按 slug 读取 Wiki 页面全文、元数据、关系链接、治理状态和来源引用。

输入：

```json
{
  "slugs": ["index", "concept/device-grading"],
  "knowledge_base_id": "可选的知识库 ID"
}
```

- `slugs`：必填字符串数组，可批量读取。
- `knowledge_base_id`：可选。省略时可能返回多个知识库里相同 slug 的页面。

常见页面类型包括 `summary`、`card`、`entity`、`concept`、`synthesis` 和 `comparison`。读取时重点检查：

- 标题、slug、摘要与正文；
- `links` 或正文中的 `[[slug]]`；
- `source_refs`；
- `knowledge_base_id`；
- 冲突、过期、审核或其他治理字段。

`index` 是知识库导航页，只显示各类节点的部分条目。未出现在索引中的节点仍可能通过 `wiki_search` 找到。

## `wiki_read_source_doc`

用途：精读 Wiki 页面引用的原始知识文档。

关键词模式：

```json
{
  "knowledge_id": "source_refs 中的知识 ID",
  "query": "质检标准|成色等级"
}
```

范围模式：

```json
{
  "knowledge_id": "source_refs 中的知识 ID",
  "start_chunk_index": 21,
  "end_chunk_index": 32
}
```

- `knowledge_id`：必填。`source_refs` 若为 `uuid|文档标题`，只取 `|` 前面的 UUID。
- `query`：可选，大小写不敏感正则；返回命中 chunk 及相邻上下文。
- `start_chunk_index`、`end_chunk_index`：可选，均为从 1 开始的 chunk 序号。
- 范围和查询不要混用；需要连续阅读时优先范围模式。
- 单次范围最多 50 个 chunk。

## 典型调用链

用户问：“卖家想尽快批量处理成色复杂的手机，应该选择哪种业务？”

1. `wiki_search`：查询 `批量.*手机`、`成色`、`快速.*处理`、业务名称同义词。
2. `wiki_read_page`：批量读取搜索命中的业务介绍、适用场景和对比节点。
3. 沿页面链接读取相关的质检或交易模式节点。
4. 若页面对时效、数量或成色限制表述不够精确，从 `source_refs` 取 `knowledge_id`，用 `wiki_read_source_doc` 搜索 `时效|批量|成色|质检`。
5. 给出业务选择、适用原因、限制条件，并标注页面 slug 和必要的源文档 chunk。

## 证据记录格式

在推理过程中为每条关键事实记录：

```text
事实：……
Wiki：页面标题（slug，knowledge_base_id）
原始依据：knowledge_id，chunk 12–14（若已核验）
状态：直接证据 / 多节点一致 / 推断 / 存在冲突
```

最终答案不必机械输出全部字段，但必须能让用户定位到关键 Wiki 节点。不要引用未实际读取的页面或 chunk。
