package agent

// Wiki ingest prompt templates for LLM-powered wiki page generation.
// These prompts are used by the wiki ingest pipeline to extract structured
// knowledge from raw documents and build/update wiki pages.

// WikiSummaryPrompt generates a summary page for a newly ingested document.
const WikiSummaryPrompt = `You are a wiki editor. Given the following document content, create a structured wiki summary page in Markdown format.

## Document Info
- Title: {{.Title}}
- File Name: {{.FileName}}
- File Type: {{.FileType}}

## Document Content
{{.Content}}

## Instructions
1. Write a comprehensive summary of the document in Markdown format.
2. Include the key facts, arguments, and conclusions.
3. Use proper heading hierarchy (## for sections, ### for subsections).
4. Where relevant, use [[wiki-link]] syntax to reference entities and concepts that might have their own wiki pages. Use lowercase slugs with hyphens, e.g. [[entity/company-name]] or [[concept/machine-learning]].
5. At the end, include a "## Key Takeaways" section with bullet points.
6. Write in {{.Language}}.
7. Keep the summary concise but thorough (500-1500 words depending on document length).

Output ONLY the Markdown content for the wiki page. Do not include any preamble or explanation.`

// WikiKnowledgeExtractPrompt extracts both entities and concepts in a single LLM call.
// Returns a JSON object with "entities" and "concepts" arrays.
// This replaces the former separate WikiEntityExtractPrompt and WikiConceptExtractPrompt.
const WikiKnowledgeExtractPrompt = `You are a knowledge extraction system. Analyze the following document and extract all significant entities AND key concepts.

## Document Info
- Title: {{.Title}}

## Document Content
{{.Content}}

## Instructions
Return a JSON object with two arrays: "entities" and "concepts".

### Entities (people, organizations, products, places, technologies, events, etc.)
Each entity should have:
- "name": The entity name (human-readable)
- "slug": URL-friendly slug, format "entity/<lowercase-hyphenated-name>"
- "description": A one-sentence description based on what the document says
- "details": A 2-5 sentence summary of key facts from the document

Only include entities that are substantively discussed (mentioned at least twice or described in detail). Do NOT include generic terms.

### Concepts (topics, themes, methodologies, theories, etc.)
Each concept should have:
- "name": The concept name (human-readable)
- "slug": URL-friendly slug, format "concept/<lowercase-hyphenated-name>"
- "description": A one-sentence definition or description
- "details": A 2-5 sentence explanation as discussed in the document

Only include concepts that are substantively discussed. Skip trivial or overly generic concepts.

### Deduplication Rules
- If something is a specific named thing (person, company, product, place), put it ONLY in "entities".
- If something is an abstract idea, methodology, or theory, put it ONLY in "concepts".
- Never duplicate items across the two arrays.

Output ONLY valid JSON. Example:
{
  "entities": [
    {
      "name": "Acme Corp",
      "slug": "entity/acme-corp",
      "description": "A technology company specializing in AI solutions.",
      "details": "Acme Corp was founded in 2020 and has grown to 500 employees. They focus on enterprise AI products and recently launched their flagship RAG platform."
    }
  ],
  "concepts": [
    {
      "name": "Retrieval-Augmented Generation",
      "slug": "concept/retrieval-augmented-generation",
      "description": "A technique that combines information retrieval with language model generation.",
      "details": "RAG works by first retrieving relevant documents from a knowledge base using vector similarity search, then feeding those documents as context to an LLM for answer generation."
    }
  ]
}`

// WikiPageUpdatePrompt incrementally updates an existing wiki page with new information.
const WikiPageUpdatePrompt = `You are a wiki editor tasked with updating an existing wiki page with new information from a recently ingested document.

## Existing Page Content
{{.ExistingContent}}

## New Information from Document "{{.NewDocTitle}}"
{{.NewContent}}

## Instructions
1. Merge the new information into the existing page content.
2. Preserve all existing information that is still valid.
3. If the new information contradicts existing content, note the contradiction explicitly: "> **Note:** This contradicts earlier information from [source]. [old claim] vs [new claim]."
4. Add new facts, details, and context from the new document.
5. Update cross-references: add new [[wiki-link]] references where appropriate.
6. Maintain the existing page structure and formatting style.
7. Add a source reference to the new document at the bottom.
8. Write in {{.Language}}.

Output ONLY the updated Markdown content. Do not include any preamble or explanation.`

// WikiIndexRebuildPrompt generates the index page content from a list of all pages.
const WikiIndexRebuildPrompt = `You are a wiki editor. Generate an index page (table of contents) for a wiki based on the following page listing.

## Pages
{{.PageListing}}

## Instructions
1. Organize pages by category (Summaries, Entities, Concepts, Analyses, etc.).
2. For each page, include: [[slug]] — one-line summary
3. Within each category, sort alphabetically.
4. Include a brief introduction at the top explaining what this wiki covers.
5. Write in {{.Language}}.

Output ONLY the Markdown content for the index page.`

// WikiLogEntryTemplate is a simple template for log entries (not LLM-generated).
const WikiLogEntryTemplate = `## [{{.Date}}] {{.Operation}} | {{.Title}}
- **Source**: {{.SourceInfo}}
- **Pages affected**: {{.PagesAffected}}
- **Summary**: {{.Summary}}
`

// WikiAgentSystemPromptAddendum is appended to the Agent system prompt when
// wiki knowledge bases are detected among the search targets.
// It tells the LLM how and when to use wiki tools.
const WikiAgentSystemPromptAddendum = `
### Wiki Knowledge Base Guidelines

You have access to a **Wiki Knowledge Base** — a persistent, interlinked collection of LLM-generated Markdown pages. The wiki is organized by page types: summaries (document summaries), entities (people, organizations, products), concepts (topics, methodologies), and special pages (index, log).

#### Retrieval Strategy (Wiki-First)
When the user's question may be answerable from the wiki:
1. **Start with the index:** Call wiki_read_index to see what knowledge pages exist and their categories.
2. **Search if needed:** Call wiki_search with keywords to find relevant pages.
3. **Deep read:** Call wiki_read_page on the most relevant slugs to get full content.
4. **Follow links:** Wiki pages contain [[slug]] cross-references. Follow them to gather related context (1-2 hops).
5. **Fall back to standard KB search** only if the wiki doesn't have sufficient information.

#### When to Write Wiki Pages
Use wiki_write_page to persist valuable knowledge artifacts. Write a page when:
- You produce a **cross-document synthesis** that combines insights from multiple sources (use page_type "synthesis")
- You generate a **comparison or evaluation** of entities, approaches, or concepts (use page_type "comparison")
- The user explicitly asks you to save analysis to the wiki

**Do NOT** write wiki pages for:
- Simple factual answers that don't add new insight
- Conversational responses (greetings, clarifications)
- Content that already exists in an existing wiki page (update it instead)

#### Page Content Guidelines
- Write in Markdown with proper heading hierarchy
- Use [[entity/slug]] and [[concept/slug]] syntax to link to other wiki pages
- Include a one-line summary in the first paragraph (used for index listings)
- Cite source documents when possible
- Keep pages focused: one topic/entity/concept per page

#### Log Page
The wiki has a log page (slug: "log") that records all ingest and update activity. Read it when the user asks about recent changes, update history, or what's new in the knowledge base.
`

// WikiDeduplicationPrompt asks the LLM to identify duplicate entities/concepts
// between newly extracted items and existing wiki pages.
const WikiDeduplicationPrompt = `You are a deduplication system. Given a list of newly extracted items and a list of existing wiki pages, determine which new items refer to the same entity or concept as an existing page.

## Newly Extracted Items
{{.NewItems}}

## Existing Wiki Pages
{{.ExistingPages}}

## Instructions
For each newly extracted item, check if it refers to the same real-world entity or concept as any existing page. Consider:
- Name variations (e.g. "Acme Corp" vs "Acme Corporation", "RAG" vs "Retrieval-Augmented Generation")
- Abbreviations and full names
- Translations (e.g. "苹果公司" vs "Apple Inc.")
- Minor spelling/formatting differences

Return a JSON object with a "merges" map. The key is the NEW item's slug, the value is the EXISTING page's slug that it should merge into. Only include items that have a match.

If no items match any existing pages, return: {"merges": {}}

Output ONLY valid JSON. Example:
{"merges": {"entity/acme-corporation": "entity/acme-corp", "concept/rag": "concept/retrieval-augmented-generation"}}`
