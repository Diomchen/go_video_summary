# Runtime Config, Obsidian Metadata, and Dynamic Prompts Design

Date: 2026-06-08

## Goal

Add a local configuration layer and richer metadata pipeline so the app can:

- Export summaries into an Obsidian vault by connecting only the vault root.
- Maintain global Markdown indexes for top-level domains and tags.
- Normalize new document domains and tags against those indexes by similarity.
- Let users configure OpenAI-compatible API providers from the Web UI.
- Let users load and switch custom summary prompts or `SKILL.md` files from the Web UI.

## Current State

The service is a single Go Web app with embedded static UI. It currently reads most settings from `.env` at startup, creates transcribers and LLM clients once in `app.NewServer`, and routes tasks through `pipeline.Manager`.

Metadata exists but is incomplete for the new behavior:

- `Task.Name`, `Task.SourceURL`, and `Task.AuthorName` cover title, source link, and UP name.
- `Task.DomainTags []string` stores one or more domain-like labels.
- Markdown/Obsidian exporters write frontmatter and relation links from the task fields.

The current Obsidian exporter can export to either `.env` vault/subdir or a per-task path. It does not maintain global indexes and does not create a single top-level domain folder per document.

## Chosen Approach

Use a runtime settings file plus two focused domain services:

- `internal/settings` owns local runtime settings, provider definitions, prompt definitions, and active selections.
- `internal/knowledge` owns Obsidian index files, similarity normalization, and target path selection.

This keeps `.env` as boot defaults while allowing the UI to update configuration without restarting for LLM provider, prompt, and Obsidian export behavior.

## Runtime Settings

Settings are stored as JSON at `outputs/_config/runtime.json` by default. The path can later be overridden with an env var if needed, but the first implementation can keep the default.

The settings schema contains:

- `obsidian.vaultDir`: the only required Obsidian path.
- `obsidian.domainIndexFile`: defaults to `领域索引.md`.
- `obsidian.tagIndexFile`: defaults to `标签索引.md`.
- `obsidian.similarityThreshold`: defaults to `0.82`.
- `llm.activeProviderID`.
- `llm.providers[]`: `id`, `name`, `baseURL`, `apiKey`, `model`, `enabled`.
- `prompts.activePromptID`.
- `prompts.items[]`: `id`, `name`, `kind`, `content`, `sourcePath`, `enabled`.

On startup, runtime settings are seeded from `.env`:

- `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` become the default provider when no runtime provider exists.
- `OBSIDIAN_VAULT_DIR` becomes the default vault root when runtime settings do not specify one.
- The built-in summary prompt remains available as a fallback prompt.

Secrets are stored locally in the runtime JSON because this is a local desktop-oriented tool. The UI should mask API keys after loading and only overwrite them when the user enters a new value.

## Metadata Model

Extend `domain.Task` with explicit metadata:

- `Title string`
- `SourceLink string`
- `UPName string`
- `Domain string`
- `Tags []string`

Compatibility:

- Existing `Name` remains the task display name.
- Existing `SourceURL` and `AuthorName` remain for old checkpoints and APIs.
- Existing `DomainTags []string` remains readable, but new code writes `Domain` and `Tags`.
- Exporters should prefer new fields and fall back to old fields.

Rules:

- A task has exactly one domain after summary metadata normalization.
- If no domain can be extracted, use `未分类`.
- Tags can be zero or more.
- Tags never create folders.

## LLM Summary and Metadata Extraction

The summarizer should return:

- Markdown summary content.
- A `SummaryMetadata` struct with title, source link, UP name, domain, and tags.

Preferred prompt contract:

The active prompt should ask the model to include a compact metadata block at the top:

```markdown
<!-- metadata
{
  "title": "...",
  "source_link": "...",
  "up_name": "...",
  "domain": "...",
  "tags": ["...", "..."]
}
-->
```

The exporter removes or ignores this HTML comment when writing final Markdown frontmatter.

Fallback extraction:

- Parse known Markdown headings such as `## 领域标签`.
- Use task fields for title/source/up when the prompt does not provide them.
- Use the first existing `DomainTags` entry as domain for older summaries.

## Obsidian Knowledge Indexes

The Obsidian vault root contains two global files:

- `领域索引.md`
- `标签索引.md`

Each file stores one item per Markdown list line:

```markdown
# 领域索引

- 科技
- 编程
- 投资
```

```markdown
# 标签索引

- Go
- OpenAI
- B站
```

Normalization flow:

1. Load the domain and tag index files from the vault root.
2. Compare the new domain against existing domains.
3. If similarity is at or above threshold, replace the new domain with the existing domain.
4. If no match exists, append the new domain.
5. Repeat the same process for each tag.
6. Persist updated indexes.

Similarity implementation:

- First implementation uses deterministic lexical similarity: normalized lowercase text, trimmed punctuation, exact match, substring bonus, and Levenshtein/Jaro-style score.
- The interface should be named so an embedding-based scorer can replace it later without touching exporters.
- This satisfies the current "vector similarity" product behavior at the boundary while avoiding a second model dependency in the first pass.

## Obsidian Export Layout

When Obsidian export is enabled, the user only chooses the vault root.

Target path:

```text
<vault>/<domain>/<safe-title>.md
```

Rules:

- Create the domain folder if needed.
- Do not create tag folders.
- Keep index files at the vault root.
- If a file exists, append a numeric suffix to avoid overwriting.

Frontmatter fields:

```yaml
---
title: "..."
source_link: "..."
up_name: "..."
domain: "科技"
tags:
  - "OpenAI"
  - "B站"
---
```

The body can still include relation links:

```markdown
## 关联

- UP主：[[UP/某UP]]
- 领域：[[科技]]
```

No tag relation section is required.

## API Design

Add settings routes:

- `GET /api/settings`
- `PUT /api/settings`
- `POST /api/settings/providers`
- `PUT /api/settings/providers/{id}`
- `DELETE /api/settings/providers/{id}`
- `POST /api/settings/providers/{id}/activate`
- `POST /api/settings/providers/{id}/test`
- `POST /api/settings/prompts`
- `PUT /api/settings/prompts/{id}`
- `DELETE /api/settings/prompts/{id}`
- `POST /api/settings/prompts/{id}/activate`
- `POST /api/settings/prompts/load-file`

The provider test route sends a small Chat Completions request to the configured base URL and model. It should report errors without changing the active provider.

Prompt load-file accepts a local path. If the file is named `SKILL.md`, store it with `kind: "skill"`. Otherwise store it as `kind: "prompt"`.

## Dynamic Runtime Use

The server should not rebuild the whole `pipeline.Manager` when provider or prompt settings change.

Instead:

- Introduce a thread-safe `settings.Store`.
- Introduce an LLM facade that implements `service.Translator` and `service.Summarizer`.
- The facade reads the active provider and prompt at each call.
- Whisper transcriber config remains startup-only for this pass.

This gives dynamic provider and prompt switching for new LLM calls, including retry-summary and future tasks.

## Web UI

Add a new top-level tab: `配置`.

Sections:

- Obsidian: vault root input, similarity threshold, index file names.
- API Provider: provider list, active provider selector, add/edit/delete provider, test button.
- Prompt/Skill: prompt list, active prompt selector, textarea editor, load local file path.

The UI should remain operational and dense like the existing console. It should not add a landing page or marketing-style layout.

## Error Handling

- Missing Obsidian vault root: Obsidian export target is unavailable.
- Index file write failure: mark Obsidian export failed with the filesystem error.
- Provider test failure: return HTTP 400 with provider response details.
- Active provider missing during summary: task enters summary failure with a clear error.
- Active prompt missing: use built-in prompt.
- Invalid prompt file path: return HTTP 400.

## Testing

Add focused Go tests for:

- Runtime settings load, defaults, save, and active selection.
- Metadata extraction from JSON comment and Markdown fallback.
- Domain/tag index parsing, append, and similarity normalization.
- Obsidian export target path and frontmatter fields.
- LLM facade choosing active provider and prompt.

Manual verification:

- Start the app.
- Open the Web UI.
- Add a provider and test it.
- Load a prompt file.
- Configure an Obsidian vault root.
- Run a short summary task or retry-summary.
- Confirm the exported note lands under `<vault>/<domain>/`.
- Confirm `领域索引.md` and `标签索引.md` are created or updated.

## Out of Scope

- Dynamic Whisper backend switching.
- Full embedding/vector database storage.
- Multi-user auth for settings APIs.
- Cloud secret storage.
- Tag folder creation.
- Automatic Obsidian app launch.
