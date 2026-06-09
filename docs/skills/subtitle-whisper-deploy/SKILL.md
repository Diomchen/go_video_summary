---
name: subtitle-whisper-deploy
description: Use when installing, deploying, starting, verifying, or troubleshooting the go_subtitle_whisper service, including faster-whisper worker setup, runtime provider configuration, Obsidian export, and local Windows/PowerShell operation.
---

# Subtitle Whisper Deploy

## Goal

Install and run `go_subtitle_whisper`, a Go web service for audio/video transcription, Bilibili URL processing, LLM summaries, and Markdown/Obsidian/Notion/IMA export.

Default deployment shape:

- One Go HTTP service.
- Optional Python `faster-whisper` worker for local GPU transcription.
- Optional OpenAI-compatible LLM provider for summaries.
- Optional Obsidian vault export using runtime settings.

Prefer local, reversible setup. Do not hardcode secrets into source files.

## First Checks

Run these from the repository root:

```powershell
git status --short --branch
go version
python --version
ffmpeg -version
```

If `ffmpeg` is missing, install it before processing non-WAV media. On Windows, `winget install Gyan.FFmpeg` is usually acceptable.

If Go is missing, install Go 1.22+.

If Python is missing, install Python 3.11+.

## Deployment Modes

Choose one transcription backend:

| Mode | Use When | Required Config |
| --- | --- | --- |
| `faster-whisper` | Local GPU is available and speed matters | Python deps, worker process, `WHISPER_FASTER_URL` |
| `openai` | Using OpenAI-compatible audio transcription API | `WHISPER_BASE_URL`, `WHISPER_API_KEY`, `WHISPER_MODEL` |
| `local` | Local whisper CLI is already installed | `WHISPER_LOCAL_BIN`, `WHISPER_LOCAL_MODEL` |

Recommended local mode is `faster-whisper`.

## Environment File

Create or update `.env` in the repository root. Keep real keys local.

Minimal local GPU configuration:

```env
HTTP_ADDR=:18880
RUNTIME_CONFIG_PATH=outputs/_config/runtime.json

WHISPER_BACKEND=faster-whisper
WHISPER_FASTER_URL=http://127.0.0.1:19000
WHISPER_FASTER_MODEL=turbo
WHISPER_FASTER_HOST=127.0.0.1
WHISPER_FASTER_PORT=19000
WHISPER_FASTER_DEVICE=cuda
WHISPER_FASTER_COMPUTE_TYPE=float16
WHISPER_FASTER_BATCH_SIZE=8
WHISPER_FASTER_NUM_WORKERS=2
WHISPER_FASTER_CPU_THREADS=2
WHISPER_FASTER_BEAM_SIZE=1
WHISPER_FASTER_VAD_FILTER=false
WHISPER_LANGUAGE=auto

LLM_BASE_URL=https://api.openai.com
LLM_API_KEY=
LLM_MODEL=gpt-4o-mini

TASK_WORKERS=1
SUMMARY_WORKERS=1
AUTO_SAVE_RESULTS=true
OUTPUT_DIR=outputs
CHECKPOINT_DIR=outputs/_checkpoints
CHUNK_SECONDS=45
CHUNK_PARALLELISM=2
BILIBILI_COOKIE_CACHE=outputs/_checkpoints/bilibili_cookie.json
BILIBILI_COOKIE_TTL=720h

OBSIDIAN_VAULT_DIR=

NOTION_TOKEN=
NOTION_PARENT_PAGE_ID=

IMA_OPENAPI_CLIENTID=
IMA_OPENAPI_APIKEY=
IMA_OPENAPI_FOLDER_ID=
```

Notes:

- `RUNTIME_CONFIG_PATH` stores API Provider, Prompt/Skill, and Obsidian settings edited from the web UI.
- `.env` seeds initial runtime settings; later web UI changes do not require restart.
- `OBSIDIAN_VAULT_DIR` should be the Obsidian vault root. The app creates domain folders and maintains `领域索引.md` / `标签索引.md`.
- `IMA_OPENAPI_FOLDER_ID` should usually stay empty unless the API behavior has been revalidated.

## Install Dependencies

For Go:

```powershell
go mod download
```

For `faster-whisper`:

```powershell
python -m pip install --upgrade pip
python -m pip install -r requirements-faster-whisper.txt
```

If CUDA or CTranslate2 errors occur, inspect the installed GPU stack before changing application code.

## Start Services

For `faster-whisper`, start the worker first in one terminal:

```powershell
python scripts/faster_whisper_worker.py
```

Expected worker endpoints:

- `GET http://127.0.0.1:19000/health`
- `POST http://127.0.0.1:19000/v1/audio/transcriptions`

Then start the Go service in another terminal:

```powershell
go run ./cmd/subtitle-whisper
```

Open:

```text
http://localhost:18880
```

If the port is busy, change `HTTP_ADDR` in `.env`.

## Runtime Configuration

After the web service starts, use the Configuration page for:

- API Provider: add OpenAI-compatible base URL, API key, and model; test and activate it.
- Prompt / Skill: paste a custom prompt or load a local `SKILL.md`; activate it for summaries and retry-summary.
- Obsidian: set vault root, index file names, and similarity threshold.

Prefer runtime settings over editing source code.

## Smoke Tests

Run these before saying deployment is ready:

```powershell
go test ./...
node --check internal/app/web/app.js
```

Health checks:

```powershell
Invoke-RestMethod http://127.0.0.1:19000/health
Invoke-RestMethod http://127.0.0.1:18880/api/health
```

Web UI checks:

1. Open `http://localhost:18880`.
2. Confirm task list loads.
3. Open Configuration.
4. Test active API Provider if summaries are required.
5. If using Obsidian, set vault root and save.
6. Submit a small local audio/video file or short Bilibili URL.
7. Confirm task progresses through transcription and, if enabled, summary/export.

## API Smoke Commands

Create a Bilibili URL task:

```powershell
$body = @{
  name = "deployment-smoke"
  urlsText = "https://www.bilibili.com/video/BV1xxxxx"
  language = "zh"
  translate = $false
  summarize = $true
  exportTargets = @("markdown", "obsidian")
  markdownExportDir = "E:\notes\whisper"
} | ConvertTo-Json

Invoke-RestMethod `
  -Uri http://127.0.0.1:18880/api/url-tasks `
  -Method Post `
  -ContentType "application/json" `
  -Body $body
```

Retry a failed whole task:

```powershell
Invoke-RestMethod -Method Post http://127.0.0.1:18880/api/tasks/<task-id>/retry
```

Retry only summary:

```powershell
Invoke-RestMethod -Method Post http://127.0.0.1:18880/api/tasks/<task-id>/retry-summary
```

Retry exports:

```powershell
$body = @{ targets = @("obsidian") } | ConvertTo-Json
Invoke-RestMethod `
  -Uri http://127.0.0.1:18880/api/tasks/<task-id>/retry-exports `
  -Method Post `
  -ContentType "application/json" `
  -Body $body
```

## Output Expectations

Local outputs go under `OUTPUT_DIR`:

- `*.transcript.txt`
- `*.translated.txt`
- `*.summary.md`

Checkpoints go under `CHECKPOINT_DIR`:

```text
outputs/_checkpoints/
  taskxxxx/
    task.json
    input.*
    input.standard.wav
    transcript.txt
    segments.json
    chunks/
```

Obsidian export writes:

- `<vault>/<领域>/<分类码-领域-标题-YYYYMMDD>.md`
- `<vault>/领域索引.md`
- `<vault>/标签索引.md`

Obsidian documents should contain frontmatter metadata and backlinks such as:

```markdown
- 领域：[[投资]]
- 标签：[[短线交易]] [[书单]]
```

## Troubleshooting

### `connectex` or timeout when calling OpenAI audio transcription

Likely network/proxy issue. Prefer one of:

- Use local `faster-whisper`.
- Configure `WHISPER_BACKEND=openai` with a reachable OpenAI-compatible transcription provider.
- Confirm the provider base URL includes only scheme and host, not `/v1`.

### `WHISPER_FASTER_URL is required`

`.env` says `WHISPER_BACKEND=faster-whisper`, but `WHISPER_FASTER_URL` is empty. Add:

```env
WHISPER_FASTER_URL=http://127.0.0.1:19000
```

### Worker starts but Go service cannot transcribe

Check:

```powershell
Invoke-RestMethod http://127.0.0.1:19000/health
```

Then confirm `.env` uses the same host and port.

### Non-WAV input fails

Install or fix `ffmpeg`. The service needs it to standardize media to 16k mono WAV.

### Summary fails but transcription succeeded

Fix the active LLM Provider in the web Configuration page, then click retry summary or call:

```powershell
Invoke-RestMethod -Method Post http://127.0.0.1:18880/api/tasks/<task-id>/retry-summary
```

### Obsidian export says exporter not configured

Set the Obsidian vault root in Configuration, or set `OBSIDIAN_VAULT_DIR` before first launch.

### Obsidian domain is too narrow

The summarizer prompt asks the model to choose one top-level domain first. The backend also normalizes common subdomains such as:

- `短线交易` / `股票` / `基金` -> `投资`
- `量化经济` / `经济分析` / `经济时政` -> `经济`
- `深度学习` / `机器学习` / `强化学习` -> `人工智能`

If the domain policy needs new categories, update the shared taxonomy code instead of patching exporter filenames directly.

## Safe Deployment Rules

- Do not commit `.env`, API keys, runtime config with secrets, checkpoint media, or generated outputs unless explicitly requested.
- Do not delete checkpoint directories while tasks may need resume.
- Do not increase `TASK_WORKERS`, `CHUNK_PARALLELISM`, or `WHISPER_FASTER_NUM_WORKERS` together without watching CPU/GPU memory.
- Prefer `go test ./...` after code changes and before declaring deployment healthy.
- Keep the worker and Go service in separate terminals unless a process manager is explicitly configured.
