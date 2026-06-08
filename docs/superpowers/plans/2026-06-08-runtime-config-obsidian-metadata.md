# Runtime Config Obsidian Metadata Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build runtime LLM/prompt settings, richer summary metadata, and Obsidian vault-root export with domain/tag indexes.

**Architecture:** Add small focused packages for settings, metadata extraction, and Obsidian knowledge normalization. Keep `.env` as startup defaults, but route LLM calls through a runtime facade that reads the active provider and active prompt for each request. Exporters prefer new metadata fields while preserving old checkpoint compatibility.

**Tech Stack:** Go standard library HTTP/JSON/filesystem, existing embedded HTML/CSS/JS frontend, existing OpenAI-compatible chat client.

---

## File Structure

- Create `internal/settings/settings.go`: runtime settings structs, defaults, JSON persistence, active provider/prompt helpers.
- Create `internal/settings/settings_test.go`: settings seed/load/save/update tests.
- Create `internal/metadata/metadata.go`: `SummaryMetadata`, metadata comment extraction, Markdown fallback extraction, task fallback helpers.
- Create `internal/metadata/metadata_test.go`: JSON comment and fallback extraction tests.
- Create `internal/knowledge/index.go`: Obsidian domain/tag index parsing, lexical similarity, normalize-and-persist behavior.
- Create `internal/knowledge/index_test.go`: index creation, append, reuse-by-similarity tests.
- Modify `internal/domain/models.go`: add explicit `Title`, `SourceLink`, `UPName`, `Domain`, `Tags`.
- Modify `internal/service/interfaces.go`: return `SummaryMetadata` from summarizer.
- Modify `internal/llm/openai.go`: split built-in prompt, allow prompt override, return structured metadata.
- Create `internal/llm/runtime.go`: runtime facade that reads active provider/prompt from `settings.Store`.
- Modify `internal/pipeline/manager.go`: persist new metadata after summary and clone/summarize new fields.
- Modify `internal/pipeline/output.go`: include new metadata in saved local summary output.
- Modify `internal/exporter/metadata.go`: frontmatter prefers new metadata fields.
- Modify `internal/exporter/obsidian.go`: export by vault root and domain folder, update indexes before writing.
- Modify `internal/exporter/ima.go` and `internal/exporter/notion.go`: prefer `Domain`/`Tags`.
- Modify `internal/app/server.go`: initialize settings store, runtime LLM facade, settings API routes.
- Modify `internal/app/web/index.html`: add `配置` top-level tab and forms.
- Modify `internal/app/web/app.js`: load/save settings, provider CRUD/test, prompt CRUD/load/activate.
- Modify `internal/app/web/styles.css`: style settings sections consistently with current console.
- Modify `README.md` and `.env.example`: document runtime config and Obsidian root behavior.

---

### Task 1: Runtime Settings Store

**Files:**
- Create: `internal/settings/settings.go`
- Test: `internal/settings/settings_test.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Write failing settings tests**

Create `internal/settings/settings_test.go` with tests for seeding from config, saving/loading JSON, preserving active provider, and masking API keys for UI output.

```go
package settings

import (
	"path/filepath"
	"testing"
	"time"

	"go_subtitle_whisper/internal/config"
)

func TestStoreSeedsFromConfigWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "runtime.json"), config.Config{
		LLMBaseURL:       "https://api.example.com",
		LLMAPIKey:        "secret",
		LLMModel:         "gpt-test",
		LLMTimeout:       3 * time.Second,
		ObsidianVaultDir: filepath.Join(dir, "vault"),
	})

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	provider, ok := got.ActiveProvider()
	if !ok {
		t.Fatalf("expected active provider")
	}
	if provider.BaseURL != "https://api.example.com" || provider.APIKey != "secret" || provider.Model != "gpt-test" {
		t.Fatalf("provider seeded incorrectly: %+v", provider)
	}
	if got.Obsidian.VaultDir != filepath.Join(dir, "vault") {
		t.Fatalf("vault dir = %q", got.Obsidian.VaultDir)
	}
	if got.Obsidian.DomainIndexFile != "领域索引.md" || got.Obsidian.TagIndexFile != "标签索引.md" {
		t.Fatalf("unexpected index names: %+v", got.Obsidian)
	}
}

func TestStoreSavesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	store := NewStore(path, config.Config{})
	settings, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	settings.LLM.Providers = append(settings.LLM.Providers, Provider{
		ID: "p1", Name: "Provider 1", BaseURL: "https://p1.example.com", APIKey: "k1", Model: "m1", Enabled: true,
	})
	settings.LLM.ActiveProviderID = "p1"
	if err := store.Save(settings); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}
	provider, ok := reloaded.ActiveProvider()
	if !ok || provider.ID != "p1" || provider.APIKey != "k1" {
		t.Fatalf("active provider not reloaded: %+v ok=%v", provider, ok)
	}
}

func TestPublicCopyMasksAPIKeys(t *testing.T) {
	settings := RuntimeSettings{
		LLM: LLMSettings{
			Providers: []Provider{{ID: "p1", APIKey: "secret"}},
		},
	}
	public := settings.PublicCopy()
	if public.LLM.Providers[0].APIKey != "********" {
		t.Fatalf("API key was not masked: %q", public.LLM.Providers[0].APIKey)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/settings`

Expected: FAIL because `internal/settings` does not exist.

- [ ] **Step 3: Implement settings store**

Create `internal/settings/settings.go` with `RuntimeSettings`, `Provider`, `Prompt`, `Store`, `Load`, `Save`, `Update`, `ActiveProvider`, `ActivePrompt`, `PublicCopy`, and default seeding from `config.Config`.

Key implementation requirements:

```go
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey,omitempty"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type Prompt struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	SourcePath string `json:"sourcePath,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type ObsidianSettings struct {
	VaultDir            string  `json:"vaultDir"`
	DomainIndexFile     string  `json:"domainIndexFile"`
	TagIndexFile        string  `json:"tagIndexFile"`
	SimilarityThreshold float64 `json:"similarityThreshold"`
}
```

Use `sync.RWMutex` in `Store`, write JSON with indentation, create parent directories before saving, and trim trailing slashes on provider base URLs.

- [ ] **Step 4: Add config field for settings path**

Modify `internal/config/config.go` to add:

```go
RuntimeConfigPath string
```

Load it with:

```go
RuntimeConfigPath: getEnv("RUNTIME_CONFIG_PATH", filepath.Join("outputs", "_config", "runtime.json")),
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/settings ./internal/config`

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/settings internal/config/config.go
git commit -m "feat: add runtime settings store"
```

---

### Task 2: Summary Metadata Model and Extraction

**Files:**
- Create: `internal/metadata/metadata.go`
- Test: `internal/metadata/metadata_test.go`
- Modify: `internal/domain/models.go`
- Modify: `internal/service/interfaces.go`

- [ ] **Step 1: Write failing metadata tests**

Create tests that parse the HTML comment contract, strip the comment, and fall back to existing Markdown heading formats.

```go
package metadata

import "testing"

func TestExtractMetadataComment(t *testing.T) {
	input := `<!-- metadata
{"title":"T","source_link":"https://example.com","up_name":"UP","domain":"科技","tags":["OpenAI","B站"]}
-->
# T

body`
	clean, meta := ExtractFromSummary(input)
	if clean == "" || clean[0] == '<' {
		t.Fatalf("metadata comment was not removed: %q", clean)
	}
	if meta.Title != "T" || meta.SourceLink != "https://example.com" || meta.UPName != "UP" || meta.Domain != "科技" {
		t.Fatalf("metadata parsed incorrectly: %+v", meta)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "OpenAI" || meta.Tags[1] != "B站" {
		t.Fatalf("tags parsed incorrectly: %#v", meta.Tags)
	}
}

func TestExtractMarkdownFallback(t *testing.T) {
	input := "# A Better Title\n\n## 领域标签\n科技 | OpenAI | B站\n\n## 核心简介\ntext"
	clean, meta := ExtractFromSummary(input)
	if clean != input {
		t.Fatalf("fallback should not rewrite content")
	}
	if meta.Title != "A Better Title" || meta.Domain != "科技" {
		t.Fatalf("fallback metadata = %+v", meta)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "OpenAI" || meta.Tags[1] != "B站" {
		t.Fatalf("fallback tags = %#v", meta.Tags)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/metadata`

Expected: FAIL because package does not exist.

- [ ] **Step 3: Implement metadata extraction**

Create `SummaryMetadata` and `ExtractFromSummary`. The JSON comment regex must match `<!-- metadata ... -->` across newlines, unmarshal into fields `title`, `source_link`, `up_name`, `domain`, and `tags`, remove the comment from returned Markdown, and sanitize empty values.

Fallback behavior:

- First `# ` heading becomes `Title`.
- `## 领域标签` line below heading is split on `|`.
- First split item becomes `Domain`; remaining split items become `Tags`.
- If no domain exists, leave it empty so the pipeline can apply `未分类`.

- [ ] **Step 4: Extend domain task fields**

Modify `internal/domain/models.go`:

```go
Title      string   `json:"title,omitempty"`
SourceLink string   `json:"sourceLink,omitempty"`
UPName     string   `json:"upName,omitempty"`
Domain     string   `json:"domain,omitempty"`
Tags       []string `json:"tags,omitempty"`
```

Add the same fields to `TaskSummary`.

- [ ] **Step 5: Change summarizer interface**

Modify `internal/service/interfaces.go`:

```go
type Summarizer interface {
	Summarize(ctx context.Context, transcript string, options SummaryOptions) (summary string, meta metadata.SummaryMetadata, err error)
}
```

Import `go_subtitle_whisper/internal/metadata`.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/metadata ./internal/domain ./internal/service`

Expected: PASS for new packages; compile failures in other packages are expected until Task 3 updates callers.

- [ ] **Step 7: Commit**

```powershell
git add internal/metadata internal/domain/models.go internal/service/interfaces.go
git commit -m "feat: add summary metadata model"
```

---

### Task 3: LLM Prompt Override and Runtime Facade

**Files:**
- Modify: `internal/llm/openai.go`
- Create: `internal/llm/runtime.go`
- Test: `internal/llm/openai_test.go`
- Test: `internal/llm/runtime_test.go`

- [ ] **Step 1: Update OpenAI client tests**

Extend existing LLM tests so `Summarize` returns cleaned Markdown and `SummaryMetadata`. Add a test server that asserts the system prompt contains custom prompt content when provided.

Test shape:

```go
func TestRuntimeClientUsesActiveProviderAndPrompt(t *testing.T) {
	// httptest server verifies model, messages, and Authorization header.
	// settings store active provider points at server.URL.
	// active prompt content is "CUSTOM PROMPT".
	// facade.Summarize returns metadata parsed from model response.
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/llm`

Expected: FAIL because interface return values and runtime facade are not implemented.

- [ ] **Step 3: Refactor OpenAI client**

Modify `internal/llm/openai.go`:

- Add `PromptOverride string` to `service.SummaryOptions` or pass override via new method `SummarizeWithPrompt`.
- Move the current hardcoded system prompt into `DefaultSummaryPrompt() string`.
- Make `Summarize` call `metadata.ExtractFromSummary(result)` and return cleaned summary plus metadata.
- Keep `rewriteSummarySourceLink`.
- Remove or replace old `extractDomainTags` usage.

- [ ] **Step 4: Add runtime facade**

Create `internal/llm/runtime.go` with:

```go
type RuntimeClient struct {
	store   *settings.Store
	timeout time.Duration
}
```

Methods:

- `Translate(ctx, input, sourceLanguage string) (string, error)`
- `Summarize(ctx, transcript string, options service.SummaryOptions) (string, metadata.SummaryMetadata, error)`
- `TestProvider(ctx context.Context, provider settings.Provider) error`

For each call, read `store.Load()`, resolve active provider, create a short-lived `Client`, and pass active prompt content into the summary call. If no active prompt exists, use `DefaultSummaryPrompt()`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/llm`

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/llm
git commit -m "feat: add runtime llm client"
```

---

### Task 4: Obsidian Knowledge Indexes

**Files:**
- Create: `internal/knowledge/index.go`
- Test: `internal/knowledge/index_test.go`

- [ ] **Step 1: Write failing index tests**

Create tests for new index files, approximate match reuse, and tag append.

```go
func TestNormalizeCreatesIndexesAndDomainFolderValue(t *testing.T) {
	vault := t.TempDir()
	idx := NewObsidianIndex(vault, "领域索引.md", "标签索引.md", 0.82)
	meta := metadata.SummaryMetadata{Domain: "科技", Tags: []string{"OpenAI", "B站"}}
	got, err := idx.Normalize(meta)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Domain != "科技" || len(got.Tags) != 2 {
		t.Fatalf("normalized = %+v", got)
	}
}

func TestNormalizeReusesSimilarDomain(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "领域索引.md"), []byte("# 领域索引\n\n- 人工智能\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := NewObsidianIndex(vault, "领域索引.md", "标签索引.md", 0.5)
	got, err := idx.Normalize(metadata.SummaryMetadata{Domain: "AI人工智能"})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Domain != "人工智能" {
		t.Fatalf("expected existing domain, got %q", got.Domain)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/knowledge`

Expected: FAIL because package does not exist.

- [ ] **Step 3: Implement index package**

Create:

- `type ObsidianIndex struct`
- `func NewObsidianIndex(vaultDir, domainFile, tagFile string, threshold float64) *ObsidianIndex`
- `func (i *ObsidianIndex) Normalize(meta metadata.SummaryMetadata) (metadata.SummaryMetadata, error)`
- `func Similarity(a, b string) float64`

Index parser reads lines beginning with `- `. Writer preserves a simple canonical heading and sorted/deduplicated list. Use `未分类` when domain is empty.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/knowledge`

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/knowledge
git commit -m "feat: add obsidian knowledge indexes"
```

---

### Task 5: Pipeline Metadata Persistence

**Files:**
- Modify: `internal/pipeline/manager.go`
- Modify: `internal/pipeline/output.go`
- Test: existing pipeline tests or compile tests

- [ ] **Step 1: Update manager compile points**

Modify `runSummaryPipeline` to receive `meta` from summarizer, then apply fallbacks:

```go
meta = metadata.MergeTaskFallbacks(meta, task)
if strings.TrimSpace(meta.Domain) == "" {
	meta.Domain = "未分类"
}
```

Update task fields:

```go
task.Title = meta.Title
task.SourceLink = meta.SourceLink
task.UPName = meta.UPName
task.Domain = meta.Domain
task.Tags = append([]string(nil), meta.Tags...)
task.DomainTags = nil
```

- [ ] **Step 2: Clone and summarize new fields**

Update `cloneTask` and `ToTaskSummary` to copy `Tags` and include `Title`, `SourceLink`, `UPName`, `Domain`.

- [ ] **Step 3: Update local output metadata**

Modify `internal/pipeline/output.go` to prefer new fields in saved `.summary.md` metadata block. Use old fields as fallback:

```go
title := firstNonEmpty(task.Title, task.Name)
sourceLink := firstNonEmpty(task.SourceLink, task.SourceURL)
upName := firstNonEmpty(task.UPName, task.AuthorName)
```

- [ ] **Step 4: Run compile tests**

Run: `go test ./internal/pipeline`

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/pipeline
git commit -m "feat: persist normalized summary metadata"
```

---

### Task 6: Exporters Prefer New Metadata and Obsidian Root Export

**Files:**
- Modify: `internal/exporter/metadata.go`
- Modify: `internal/exporter/obsidian.go`
- Modify: `internal/exporter/obsidian_test.go`
- Modify: `internal/exporter/markdown_test.go`
- Modify: `internal/exporter/ima.go`
- Modify: `internal/exporter/notion.go`

- [ ] **Step 1: Update Obsidian tests**

Add a test that configures only vault root, exports a task with `Domain: "科技"` and `Tags`, then asserts:

- File path is `<vault>/科技/<filename>.md`.
- `领域索引.md` contains `科技`.
- `标签索引.md` contains each tag.
- Frontmatter includes `domain: "科技"` and `tags`.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/exporter`

Expected: FAIL because exporter still writes to vault/subdir and old fields.

- [ ] **Step 3: Update metadata frontmatter**

Modify `buildMetadataMarkdownContent` to emit:

- `title`
- `source_link`
- `up_name`
- `domain`
- `tags`

Keep old fields such as `source_url`, `author`, `up`, and `domain_tags` only as compatibility aliases if old task fields exist and new fields are empty.

- [ ] **Step 4: Update Obsidian exporter**

Modify `ObsidianExporter` to optionally hold:

```go
domainIndexFile string
tagIndexFile string
similarityThreshold float64
```

Add constructor:

```go
func NewObsidianVaultExporter(vaultDir string, index knowledge.ObsidianIndex) *ObsidianExporter
```

Before writing, normalize metadata through `internal/knowledge`, update task fields locally for export content, then write under `<vault>/<domain>/`.

- [ ] **Step 5: Update Notion and IMA metadata lines**

Replace `task.DomainTags` display with `task.Domain` and `task.Tags`, falling back to `DomainTags` only if new fields are empty.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/exporter ./internal/knowledge`

Expected: PASS.

- [ ] **Step 7: Commit**

```powershell
git add internal/exporter
git commit -m "feat: export obsidian notes by domain"
```

---

### Task 7: Settings APIs and Server Wiring

**Files:**
- Modify: `internal/app/server.go`
- Test: optional `internal/app/server_test.go`

- [ ] **Step 1: Add server fields**

Add:

```go
settingsStore *settings.Store
runtimeLLM    *llm.RuntimeClient
```

In `NewServer`, create `settings.NewStore(cfg.RuntimeConfigPath, cfg)`, load once to seed, and use `llm.NewRuntimeClient(settingsStore, cfg.LLMTimeout)` for translator/summarizer.

- [ ] **Step 2: Wire Obsidian exporter from settings**

Use a dynamic exporter or manager exporter lookup that can read runtime vault settings. The simplest first pass is to register a lightweight exporter whose `ExportMarkdown` loads current settings and delegates to `exporter.NewObsidianExporter`.

- [ ] **Step 3: Add routes**

Register:

```go
mux.HandleFunc("/api/settings", s.handleSettings)
mux.HandleFunc("/api/settings/providers", s.handleSettingsProviders)
mux.HandleFunc("/api/settings/providers/", s.handleSettingsProviderByID)
mux.HandleFunc("/api/settings/prompts", s.handleSettingsPrompts)
mux.HandleFunc("/api/settings/prompts/", s.handleSettingsPromptByID)
```

- [ ] **Step 4: Implement handlers**

Handlers must:

- Return `settings.PublicCopy()` for GET.
- Merge masked API key updates so `"********"` keeps the previous key.
- Create stable IDs using timestamp plus random suffix.
- Activate provider/prompt by ID.
- Test provider with `runtimeLLM.TestProvider`.
- Load prompt file by path with `os.ReadFile`; `SKILL.md` maps to kind `skill`.

- [ ] **Step 5: Run app compile tests**

Run: `go test ./internal/app ./internal/llm ./internal/settings`

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/app
git commit -m "feat: add runtime settings api"
```

---

### Task 8: Web UI Configuration Tab

**Files:**
- Modify: `internal/app/web/index.html`
- Modify: `internal/app/web/app.js`
- Modify: `internal/app/web/styles.css`

- [ ] **Step 1: Add settings tab markup**

Add a `配置` tab next to existing tabs and a panel containing:

- Obsidian vault root and threshold form.
- Provider list and editor form.
- Prompt list and editor/load form.

- [ ] **Step 2: Implement JS state and API calls**

Add functions:

```js
async function loadSettings()
async function saveSettingsPatch(patch)
async function createProvider(provider)
async function updateProvider(id, provider)
async function activateProvider(id)
async function testProvider(id)
async function createPrompt(prompt)
async function updatePrompt(id, prompt)
async function activatePrompt(id)
async function loadPromptFile(path)
```

Render active provider/prompt with a clear active chip. Use masked key behavior: if the field value remains `********`, send it unchanged and let the server preserve the old key.

- [ ] **Step 3: Add CSS**

Use existing `surface`, `field`, `ghost-btn`, and `primary-btn` styles. Add only compact grid/list classes needed for settings. Keep cards flat and avoid nested card styling.

- [ ] **Step 4: Manual browser check**

Run: `go run ./cmd/subtitle-whisper`

Open: `http://localhost:18880`

Expected:

- `配置` tab appears.
- Settings load without JS console errors.
- Obsidian settings save.
- Provider can be added and activated.
- Prompt can be added and activated.

- [ ] **Step 5: Commit**

```powershell
git add internal/app/web
git commit -m "feat: add runtime settings ui"
```

---

### Task 9: Docs, Compatibility, and Final Verification

**Files:**
- Modify: `README.md`
- Modify: `.env.example`
- Possibly modify tests touched by compile changes.

- [ ] **Step 1: Update docs**

Document:

- `RUNTIME_CONFIG_PATH`
- Obsidian now only needs vault root.
- Domain folder layout.
- `领域索引.md` and `标签索引.md`.
- Runtime provider and prompt configuration.

- [ ] **Step 2: Full test suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Manual smoke test**

Run the app, configure settings, and submit or retry a summary task. Verify the runtime config JSON is written and Obsidian export updates indexes.

- [ ] **Step 4: Commit**

```powershell
git add README.md .env.example
git commit -m "docs: document runtime obsidian settings"
```

- [ ] **Step 5: Final status**

Run:

```powershell
git status --short
git log --oneline -5
```

Expected: clean worktree except for intentionally generated local runtime files that are ignored or left unstaged.

---

## Self-Review

- Spec coverage: runtime settings, metadata model, Obsidian indexes, dynamic providers, dynamic prompts, API routes, UI, error handling, and tests are all mapped to tasks.
- Scope: dynamic Whisper switching, embedding database, auth, cloud secrets, tag folders, and Obsidian app launch remain out of scope.
- Type consistency: `settings.Provider`, `settings.Prompt`, `metadata.SummaryMetadata`, `settings.Store`, `llm.RuntimeClient`, `Task.Domain`, and `Task.Tags` are the central names used throughout.
