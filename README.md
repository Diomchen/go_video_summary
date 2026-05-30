# go_subtitle_whisper

一个用 Go 编写的音视频转写工具，当前聚焦“文件 + 哔哩哔哩链接”批量处理场景：

1. 本地音视频文件批量上传
2. 哔哩哔哩视频链接批量解析，一行一个链接
3. 哔哩哔哩稍后再看列表扫码登录解析
4. 按队列顺序执行，支持任务断点续跑
5. 支持 faster-whisper、本地 Whisper 与 OpenAI Whisper
6. 可选 LLM 翻译与 Markdown 总结
7. 总结结果可自动导入 Markdown 文件夹、Notion、Obsidian、IMA
8. 总结失败后可单独重试，不必重新跑 Whisper

## 运行展示
![](https://cdn.jsdelivr.net/gh/Diomchen/pic2.0@main/img/20260525181031319.png)

![](https://cdn.jsdelivr.net/gh/Diomchen/pic2.0@main/img/20260525181215949.png)

![](https://cdn.jsdelivr.net/gh/Diomchen/pic2.0@main/img/20260525181307799.png)

![](https://cdn.jsdelivr.net/gh/Diomchen/pic2.0@main/img/20260525181331251.png)


## 当前能力

- 本地文件任务与 B 站链接任务共用同一队列
- B 站链接处理流程：解析页面 -> 下载音频流 -> 转写 -> 清理临时媒体文件
- 非 WAV 文件会统一转为 16k 单声道 WAV 后分块处理
- 断点续跑基于检查点目录：
  - 原始输入文件和中间状态保存在 `CHECKPOINT_DIR`
  - 每完成一个分块就写入 `transcript.txt` 与 `segments.json`
  - 服务重启后会从已完成分块继续
- 开启总结后会生成 `.summary.md`
- 配好导出目标后可自动导出到普通 Markdown 文件夹、Notion、Obsidian、IMA

## 推荐配置

```env
HTTP_ADDR=:18880

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
LLM_API_KEY=your_key
LLM_MODEL=gpt-4o-mini

TASK_WORKERS=1
AUTO_SAVE_RESULTS=true
OUTPUT_DIR=outputs
CHECKPOINT_DIR=outputs/_checkpoints
CHUNK_SECONDS=45
CHUNK_PARALLELISM=2
BILIBILI_COOKIE_CACHE=outputs/_checkpoints/bilibili_cookie.json
BILIBILI_COOKIE_TTL=720h

NOTION_TOKEN=
NOTION_PARENT_PAGE_ID=

OBSIDIAN_VAULT_DIR=
OBSIDIAN_SUBDIR=Whisper Imports

IMA_OPENAPI_CLIENTID=
IMA_OPENAPI_APIKEY=
IMA_OPENAPI_FOLDER_ID=
```

说明：

- `WHISPER_BACKEND=faster-whisper`：推荐本地 GPU 方案，Go 服务会把 chunk 发给常驻的 Python worker。
- `WHISPER_FASTER_URL`：Go 服务访问 worker 的地址。
- `WHISPER_FASTER_MODEL=turbo`：4070 Ti 这类显卡建议先从 `turbo` 开始。
- `WHISPER_FASTER_BATCH_SIZE=8`：单请求内的 batched inference，适合提高 GPU 利用率。
- `WHISPER_FASTER_NUM_WORKERS=2`：允许 worker 同时处理多个请求，越高显存占用越大。
- `WHISPER_FASTER_CPU_THREADS=2`：限制 CPU 线程，避免回到 CPU 95C+ 的状态。
- `TASK_WORKERS=1`：默认串行，更适合本地模型避免资源竞争。
- `CHECKPOINT_DIR`：断点续跑状态、任务元数据和中间文本的保存目录。
- `CHUNK_SECONDS=45`：每 45 秒切一块，块越小越利于恢复，但任务会更碎。
- `CHUNK_PARALLELISM=2`：单任务内 chunk 并发数；faster-whisper 常驻 worker 下建议先从 `2` 开始试，再升到 `3`。
- `BILIBILI_COOKIE_CACHE`：B 站扫码登录后 cookie 的本地缓存文件，用于解析稍后再看列表。
- `BILIBILI_COOKIE_TTL=720h`：登录态缓存时长，默认 30 天；过期或接口返回未登录时会重新扫码。
- `NOTION_PARENT_PAGE_ID`：当前实现为“在指定 Notion 页面下创建子页面”。
- `OBSIDIAN_VAULT_DIR`：填写默认 Obsidian Vault 根目录；也可以在 Web 页面为单次任务手动填写 Obsidian 导出目录。
- `IMA_OPENAPI_CLIENTID` 和 `IMA_OPENAPI_APIKEY`：从 [IMA 开放接口页面](https://ima.qq.com/agent-interface) 获取。
- `IMA_OPENAPI_FOLDER_ID`：建议默认留空。你已经验证过，把知识库文档目录对应的 `folder_id` 填进去会失败；当前推荐流程是先导入普通笔记，再在 IMA 里手动转存到知识库文档下。

## faster-whisper Worker

先安装 Python 依赖：

```powershell
python -m pip install -r requirements-faster-whisper.txt
```

然后在项目根目录启动本地 worker：

```powershell
python scripts/faster_whisper_worker.py
```

worker 会自动读取当前目录下的 `.env`，并暴露一个 OpenAI 兼容接口：

- `GET /health`
- `POST /v1/audio/transcriptions`

这样 Go 侧不用改任务和 checkpoint 结构，只是把每个 chunk 发给本地 GPU worker。

## 运行

```powershell
go run ./cmd/subtitle-whisper
```

打开 [http://localhost:18880](http://localhost:18880)

## 文档

- 架构说明与调用流程：`docs/ARCHITECTURE.md`

## 外部接口

### 1. 提交本地文件任务

`POST /api/tasks`

使用 `multipart/form-data`：

- `file`: 可多文件
- `name`: 可选，任务名前缀
- `language`: 可选
- `translate`: `true/false`
- `summarize`: `true/false`

### 2. 提交哔哩哔哩链接任务

`POST /api/url-tasks`

```json
{
  "name": "课程整理",
  "urlsText": "https://www.bilibili.com/video/BV1xxxxx\nhttps://www.bilibili.com/video/BV2yyyyy",
  "language": "zh",
  "translate": false,
  "summarize": true,
  "exportTargets": ["markdown", "obsidian"],
  "markdownExportDir": "E:\\notes\\whisper",
  "obsidianExportDir": "E:\\Obsidian\\MyVault\\B站转写"
}
```

也支持：

```json
{
  "urls": [
    "https://www.bilibili.com/video/BV1xxxxx",
    "https://www.bilibili.com/video/BV2yyyyy"
  ]
}
```

提交稍后再看列表链接时也走同一接口：

```json
{
  "urlsText": "https://www.bilibili.com/watchlater/list#/list",
  "summarize": true
}
```

Web 页面会在首次使用或登录态过期时弹出 B 站扫码登录框。登录成功后 cookie 会缓存在 `BILIBILI_COOKIE_CACHE`，默认 30 天内不重复登录。

导出说明：

- `markdownExportDir`：勾选 `markdown` 时，把总结 Markdown 写入指定普通文件夹。
- `obsidianExportDir`：勾选 `obsidian` 时，本次任务优先写入该目录；为空时使用 `.env` 中的 `OBSIDIAN_VAULT_DIR` / `OBSIDIAN_SUBDIR`。
- Obsidian 导出的 Markdown 会写入 `tags`、`domain_tags`、`up`、`bvid`、`collection` 等 frontmatter，并在正文开头增加 `[[UP/...]]`、`[[领域/...]]`、`[[合集/...]]` 形式的关系链接，方便关系图谱聚合。

### 3. 查询任务

- `GET /api/tasks`
- `GET /api/tasks/{id}`
- `GET /callback/tasks/{id}`：对外开放的轻量任务状态查询接口，只返回基础状态信息
- `GET /api/events`：SSE 事件流

`GET /callback/tasks/{id}` 返回示例：

```json
{
  "id": "taska8k2m4p9x1z7",
  "name": "课程整理 - demo.mp4",
  "mode": "file",
  "status": "running",
  "stage": "transcribing",
  "progressPercent": 42.5,
  "createdAt": "2026-03-22T11:23:45+08:00",
  "updatedAt": "2026-03-22T11:24:18+08:00"
}
```

### 4. 重试总结

当转写成功但总结失败时，可调用：

`POST /api/tasks/{id}/retry-summary`

## 输出结果

当任务完成后：

- 转写文本：`*.transcript.txt`（`AUTO_SAVE_RESULTS=true` 时）
- 翻译文本：`*.translated.txt`（`AUTO_SAVE_RESULTS=true` 时）
- 总结文档：`*.summary.md`（只要有总结内容就会保存）

如果配置了 IMA OpenAPI：

- 总结 Markdown 会调用 `import_doc` 接口创建一篇新笔记
- 成功后任务导出状态里会记录返回的 `doc_id`

## 依赖

- faster-whisper 模式：需要 Python 3.11+ 和 `pip install -r requirements-faster-whisper.txt`
- 本地 Whisper 模式：需要本地 `whisper` 可执行文件和模型文件
- 建议系统可用 `ffmpeg`，用于非 WAV 输入和音频标准化
- Notion 自动导入：需要有效的 Integration Token，并把目标页面共享给该 Integration
- IMA 自动导入：需要有效的 `IMA_OPENAPI_CLIENTID` 和 `IMA_OPENAPI_APIKEY`
