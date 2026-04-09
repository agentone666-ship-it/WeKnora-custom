package agent

// Wiki ingest prompt templates for LLM-powered wiki page generation.
// These prompts are used by the wiki ingest pipeline to extract structured
// knowledge from raw documents and build/update wiki pages.

// WikiSummaryPrompt generates a summary page for a newly ingested document.
const WikiSummaryPrompt = `You are a wiki editor. Given the following document content, create a structured wiki summary page in Markdown format.

<document>
<title>{{.Title}}</title>
<file_name>{{.FileName}}</file_name>
<file_type>{{.FileType}}</file_type>
<content>
{{.Content}}
</content>
</document>

<available_wiki_pages>
{{.ExtractedSlugs}}
</available_wiki_pages>

<instructions>
1. The FIRST line of your output MUST be: SUMMARY: {one sentence, 15-40 words, describing what this document is about — for wiki index listing}
2. After the SUMMARY line, write a comprehensive summary of the document in Markdown format.
3. Include the key facts, arguments, and conclusions.
4. Use proper heading hierarchy (## for sections, ### for subsections).
5. **Wiki-link rule**: The available_wiki_pages list above maps slugs to display names and their aliases (format: "[[slug]] = display name (Aliases: a, b)"). Whenever you mention a name or alias that matches a listed entry, you MUST write it as [[slug|display name]] (e.g. [[entity/zhong-guo|中国]]), NOT as bold (**name**) or bare [[slug]]. Use the EXACT slugs provided — do NOT invent new slugs.
6. **Image rule**: If the document contains <images> tags with <image> elements, you SHOULD include the relevant images in your summary using the Markdown syntax: ![caption](url). Place the images where they are contextually relevant to the text.
7. At the end, include a "## Key Takeaways" section with bullet points.
8. Write in {{.Language}}.
9. Keep the summary concise but thorough (500-1500 words depending on document length).
</instructions>

Output the SUMMARY line first, then the Markdown content. Do not include any other preamble.`

// WikiKnowledgeExtractPrompt extracts both entities and concepts in a single LLM call.
// Returns a JSON object with "entities" and "concepts" arrays.
// This replaces the former separate WikiEntityExtractPrompt and WikiConceptExtractPrompt.
const WikiKnowledgeExtractPrompt = `You are a knowledge extraction system. Analyze the following document and extract all significant entities AND key concepts.

<document>
<title>{{.Title}}</title>
<content>
{{.Content}}
</content>
</document>

<previous_slugs>
{{.PreviousSlugs}}
</previous_slugs>

<instructions>
Return a JSON object with two arrays: "entities" and "concepts".
**IMPORTANT: Write ALL names, descriptions, and details in {{.Language}}**.

### Slug Continuity Rules
If previous slugs are provided above, you MUST follow these rules:
- If an entity or concept from the previous extraction still exists in the current document, **reuse its exact slug** from the previous list. Do NOT generate a new slug for the same thing.
- If an entity or concept no longer appears in the document, **do NOT include it** in the output.
- Only generate new slugs for entities/concepts that are genuinely new (not present in the previous list).
- This ensures slug stability across document updates.

### Entities (people, organizations, products, places, technologies, events, etc.)
Each entity should have:
- "name": The entity name in {{.Language}} (human-readable)
- "slug": URL-friendly slug, format "entity/<lowercase-hyphenated-name>" (use romanized/pinyin form for non-Latin names). **Reuse previous slug if the entity was extracted before.**
- "aliases": An array of strings representing alternative names, abbreviations, acronyms or translations of the entity found in the document. Provide [] if none.
- "description": **Index listing summary** — one sentence, 15-40 words, in {{.Language}}. Describes WHAT this entity IS and its role in the document. Must be self-contained (understandable without reading the full page). This will be displayed in the wiki index.
- "details": A 2-5 sentence summary in {{.Language}} of key facts from the document. **Image rule**: If the document contains relevant <image> elements in an <images> tag, include them in the details using Markdown syntax: ![caption](url).

Only include entities that are substantively discussed (mentioned at least twice or described in detail). Do NOT include generic terms.

### Concepts (topics, themes, methodologies, theories, etc.)
Each concept should have:
- "name": The concept name in {{.Language}} (human-readable)
- "slug": URL-friendly slug, format "concept/<lowercase-hyphenated-name>" (use romanized/pinyin form for non-Latin names). **Reuse previous slug if the concept was extracted before.**
- "aliases": An array of strings representing alternative names, abbreviations, acronyms or translations of the concept found in the document. Provide [] if none.
- "description": **Index listing summary** — one sentence, 15-40 words, in {{.Language}}. Defines WHAT this concept IS. Must be self-contained (understandable without reading the full page). This will be displayed in the wiki index.
- "details": A 2-5 sentence explanation in {{.Language}} as discussed in the document. **Image rule**: If the document contains relevant <image> elements in an <images> tag, include them in the details using Markdown syntax: ![caption](url).

Only include concepts that are substantively discussed. Skip trivial or overly generic concepts.

### Deduplication Rules
- If something is a specific named thing (person, company, product, place), put it ONLY in "entities".
- If something is an abstract idea, methodology, or theory, put it ONLY in "concepts".
- Never duplicate items across the two arrays.

### JSON Formatting Rules
- **CRITICAL**: Do NOT use literal newline characters inside JSON string values. If you need a newline in a string, you MUST use the escaped sequence \n.
</instructions>

Output ONLY valid JSON. Example:
{
  "entities": [
    {
      "name": "Acme Corp",
      "slug": "entity/acme-corp",
      "aliases": ["Acme", "Acme Corporation"],
      "description": "A technology company specializing in AI solutions.",
      "details": "Acme Corp was founded in 2020 and has grown to 500 employees. They focus on enterprise AI products and recently launched their flagship RAG platform."
    }
  ],
  "concepts": [
    {
      "name": "Retrieval-Augmented Generation",
      "slug": "concept/retrieval-augmented-generation",
      "aliases": ["RAG"],
      "description": "A technique that combines information retrieval with language model generation.",
      "details": "RAG works by first retrieving relevant documents from a knowledge base using vector similarity search, then feeding those documents as context to an LLM for answer generation."
    }
  ]
}`

// WikiPageModifyPrompt updates an existing wiki page with new additions and removes stale/deleted information in a single pass.
const WikiPageModifyPrompt = `You are a wiki editor tasked with updating an existing wiki page. You must process a set of NEW information to add, AND/OR a set of deleted documents whose exclusive contributions must be REMOVED.

<existing_page_content>
{{.ExistingContent}}
</existing_page_content>

{{if .HasAdditions}}
<new_information>
{{.NewContent}}
</new_information>
{{end}}

{{if .HasRetractions}}
<deleted_documents>
{{.DeletedContent}}
</deleted_documents>

<remaining_source_documents>
{{.RemainingSourcesContent}}
</remaining_source_documents>
{{end}}

<valid_wiki_links>
{{.AvailableSlugs}}
</valid_wiki_links>

<instructions>
1. The FIRST line of your output MUST be: SUMMARY: {one sentence, 15-40 words, describing what this page is about after the update — for wiki index listing}
{{if .HasRetractions}}
2. REMOVE facts/claims that were ONLY sourced from the <deleted_documents> and are NOT present in any <remaining_source_documents> or <new_information>.
{{end}}
{{if .HasAdditions}}
3. ADD and MERGE the facts, details, and context from the <new_information> into the page. If it contradicts old content, prefer the newer information.
{{end}}
4. Preserve existing information that is still valid.
5. Keep [[slug|name]] wiki-link references ONLY if the slug appears in the <valid_wiki_links> list above. Remove any [[slug|name]] whose slug is NOT in that list. Do NOT invent new wiki-link slugs.
6. Maintain the existing page structure and formatting style.
7. **Image rule**: Include relevant images using Markdown syntax: ![caption](url) from new information if applicable.
{{if .HasRetractions}}
8. If after removing deleted content the page becomes nearly empty and there is no new information to add, output just: "SUMMARY: (empty page)\n# [Title]\n\n*This page's primary source document was removed.*"
{{end}}
9. Write in {{.Language}}.
</instructions>

Output the SUMMARY line first, then the updated Markdown content. Do not include any other preamble.`

// WikiIndexIntroPrompt generates the introduction for a NEW index page (first time only).
const WikiIndexIntroPrompt = `You are a wiki editor. Write a brief introduction for a wiki knowledge base index page.

<document_summaries>
{{.DocumentSummaries}}
</document_summaries>

<instructions>
1. Write a title line starting with "# " that reflects the knowledge domain.
2. Follow with 2-3 sentences describing what this wiki covers, based on the document summaries above.
3. Keep it concise — this is just the header section, the directory listing will be added separately below.
4. Write in {{.Language}}.
</instructions>

Output ONLY the title and introduction paragraph. Do NOT generate any directory listings or page links.`

// WikiIndexIntroUpdatePrompt incrementally updates an existing index introduction.
const WikiIndexIntroUpdatePrompt = `You are a wiki editor. Update the introduction section of a wiki index page to reflect recent changes.

<current_introduction>
{{.ExistingIntro}}
</current_introduction>

<changes>
{{.ChangeDescription}}
</changes>

<document_summaries>
{{.DocumentSummaries}}
</document_summaries>

<instructions>
1. Update the introduction to accurately reflect the current state of the wiki.
2. If documents were added, mention the new topics if they significantly change the wiki's scope.
3. If documents were removed, remove references to those topics if they no longer apply.
4. Keep the same tone, style, and title format as the existing introduction.
5. Keep it concise — 1 title line + 2-3 sentences.
6. Write in {{.Language}}.
</instructions>

Output ONLY the updated title and introduction paragraph. Do NOT generate any directory listings or page links.`

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
- Use [[slug|display name]] syntax to link to other wiki pages (e.g. [[entity/acme-corp|Acme Corp]])
- Include a one-line summary in the first paragraph (used for index listings)
- Cite source documents when possible
- Keep pages focused: one topic/entity/concept per page

#### Log Page
The wiki has a log page (slug: "log") that records all ingest and update activity. Read it when the user asks about recent changes, update history, or what's new in the knowledge base.
`

// WikiDeduplicationPrompt asks the LLM to identify duplicate entities/concepts
// between newly extracted items and existing wiki pages.
const WikiDeduplicationPrompt = `You are a deduplication system. Given a list of newly extracted items and a list of existing wiki pages, determine which new items refer to the same entity or concept as an existing page.

<new_items>
{{.NewItems}}
</new_items>

<existing_pages>
{{.ExistingPages}}
</existing_pages>

<instructions>
For each newly extracted item, check if it refers to the same real-world entity or concept as any existing page. Consider:
- Name variations (e.g. "Acme Corp" vs "Acme Corporation", "RAG" vs "Retrieval-Augmented Generation")
- Abbreviations and full names
- Translations (e.g. "苹果公司" vs "Apple Inc.")
- Minor spelling/formatting differences

Return a JSON object with a "merges" map. The key is the NEW item's slug, the value is the EXISTING page's slug that it should merge into. Only include items that have a match.

If no items match any existing pages, return: {"merges": {}}

### JSON Formatting Rules
- **CRITICAL**: Do NOT use literal newline characters inside JSON string values. If you need a newline in a string, you MUST use the escaped sequence \n.
</instructions>

Output ONLY valid JSON. Example:
{"merges": {"entity/acme-corporation": "entity/acme-corp", "concept/rag": "concept/retrieval-augmented-generation"}}`
