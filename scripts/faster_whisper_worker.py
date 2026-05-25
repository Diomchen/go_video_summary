#!/usr/bin/env python3

from __future__ import annotations

import json
import logging
import os
import sys
import tempfile
from email.parser import BytesParser
from email.policy import default as email_policy
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

try:
    from faster_whisper import BatchedInferencePipeline, WhisperModel
except ModuleNotFoundError as exc:
    if exc.name == "faster_whisper":
        raise SystemExit(
            "faster_whisper is not installed in the current Python environment.\n"
            f"Current interpreter: {sys.executable}\n"
            "Install it in this environment with:\n"
            "  python -m pip install -r requirements-faster-whisper.txt"
        ) from exc
    raise


def load_dotenv(path: str) -> None:
    file_path = Path(path)
    if not file_path.exists():
        return

    for raw_line in file_path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[len("export ") :]
        if "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip("'").strip('"')
        if key:
            os.environ.setdefault(key, value)


def get_env(name: str, default: str) -> str:
    value = os.getenv(name, "")
    value = value.strip()
    if value:
        return value
    return default


def get_int_env(name: str, default: int) -> int:
    try:
        return int(get_env(name, str(default)))
    except ValueError:
        return default


def get_bool_env(name: str, default: bool) -> bool:
    value = get_env(name, "")
    if not value:
        return default
    return value.lower() in {"1", "true", "yes", "on"}


class FasterWhisperWorker:
    def __init__(self) -> None:
        load_dotenv(".env")

        self.model_name = get_env("WHISPER_FASTER_MODEL", "turbo")
        self.device = get_env("WHISPER_FASTER_DEVICE", "cuda")
        self.compute_type = get_env("WHISPER_FASTER_COMPUTE_TYPE", "float16")
        self.batch_size = max(1, get_int_env("WHISPER_FASTER_BATCH_SIZE", 8))
        self.num_workers = max(1, get_int_env("WHISPER_FASTER_NUM_WORKERS", 2))
        self.cpu_threads = max(1, get_int_env("WHISPER_FASTER_CPU_THREADS", 2))
        self.beam_size = max(1, get_int_env("WHISPER_FASTER_BEAM_SIZE", 1))
        self.vad_filter = get_bool_env("WHISPER_FASTER_VAD_FILTER", False)
        self.host = get_env("WHISPER_FASTER_HOST", "127.0.0.1")
        self.port = max(1, get_int_env("WHISPER_FASTER_PORT", 19000))
        self.download_root = get_env("WHISPER_FASTER_DOWNLOAD_ROOT", "")

        model_kwargs = {
            "device": self.device,
            "compute_type": self.compute_type,
            "cpu_threads": self.cpu_threads,
            "num_workers": self.num_workers,
        }
        if self.download_root:
            model_kwargs["download_root"] = self.download_root

        logging.info(
            "loading faster-whisper model=%s device=%s compute_type=%s batch_size=%s num_workers=%s cpu_threads=%s",
            self.model_name,
            self.device,
            self.compute_type,
            self.batch_size,
            self.num_workers,
            self.cpu_threads,
        )
        self.model = WhisperModel(self.model_name, **model_kwargs)
        self.pipeline = BatchedInferencePipeline(model=self.model) if self.batch_size > 1 else None

    def health(self) -> dict:
        return {
            "ok": True,
            "model": self.model_name,
            "device": self.device,
            "computeType": self.compute_type,
            "batchSize": self.batch_size,
            "numWorkers": self.num_workers,
            "cpuThreads": self.cpu_threads,
            "vadFilter": self.vad_filter,
        }

    def transcribe(self, file_path: str, language: str) -> dict:
        options = {
            "beam_size": self.beam_size,
            "vad_filter": self.vad_filter,
            "condition_on_previous_text": False,
            "word_timestamps": False,
        }
        language = (language or "").strip()
        if language and language.lower() != "auto":
            options["language"] = language

        segments, info = self._run_transcribe(file_path, options)

        text_parts = []
        payload_segments = []
        for segment in segments:
            segment_text = (segment.text or "").strip()
            if not segment_text:
                continue
            text_parts.append(segment_text)
            payload_segments.append(
                {
                    "start": segment.start,
                    "end": segment.end,
                    "text": segment_text,
                }
            )

        return {
            "text": " ".join(text_parts).strip(),
            "language": getattr(info, "language", None),
            "language_probability": getattr(info, "language_probability", None),
            "duration": getattr(info, "duration", None),
            "segments": payload_segments,
        }

    def _run_transcribe(self, file_path: str, options: dict):
        if self.pipeline is not None:
            try:
                batched_options = dict(options)
                # BatchedInferencePipeline expects clip timestamps for long audio. Enabling
                # VAD here makes the batched path much more robust for chunked requests.
                batched_options["vad_filter"] = True
                return self.pipeline.transcribe(file_path, batch_size=self.batch_size, **batched_options)
            except Exception as exc:  # noqa: BLE001
                # BatchedInferencePipeline can fail on low-speech chunks when it cannot derive
                # clip timestamps. Fall back to the plain model path for robustness.
                if "No clip timestamps found" not in str(exc):
                    raise
                logging.warning("batched transcription fallback: %s", exc)
                fallback_options = dict(options)
                fallback_options.pop("vad_filter", None)
                return self.model.transcribe(file_path, **fallback_options)

        return self.model.transcribe(file_path, **options)


WORKER = FasterWhisperWorker()


def parse_multipart(headers, body: bytes) -> tuple[dict[str, str], bytes, str]:
    content_type = headers.get("Content-Type", "")
    message = BytesParser(policy=email_policy).parsebytes(
        f"Content-Type: {content_type}\r\nMIME-Version: 1.0\r\n\r\n".encode("utf-8") + body
    )

    fields: dict[str, str] = {}
    file_content = b""
    filename = "audio.wav"

    for part in message.iter_parts():
        name = part.get_param("name", header="content-disposition")
        if not name:
            continue

        payload = part.get_payload(decode=True) or b""
        part_filename = part.get_filename()
        if part_filename is not None:
            file_content = payload
            filename = Path(part_filename).name or filename
            continue

        charset = part.get_content_charset() or "utf-8"
        fields[name] = payload.decode(charset, errors="replace").strip()

    return fields, file_content, filename


class Handler(BaseHTTPRequestHandler):
    server_version = "faster-whisper-worker/1.0"

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/health":
            self.write_json(HTTPStatus.OK, WORKER.health())
            return
        self.write_json(HTTPStatus.NOT_FOUND, {"error": "not found"})

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/audio/transcriptions":
            self.write_json(HTTPStatus.NOT_FOUND, {"error": "not found"})
            return

        content_type = self.headers.get("Content-Type", "")
        if "multipart/form-data" not in content_type:
            self.write_json(HTTPStatus.BAD_REQUEST, {"error": "multipart/form-data is required"})
            return

        content_length = int(self.headers.get("Content-Length", "0") or "0")
        if content_length <= 0:
            self.write_json(HTTPStatus.BAD_REQUEST, {"error": "request body is empty"})
            return

        fields, file_bytes, filename = parse_multipart(self.headers, self.rfile.read(content_length))
        if not file_bytes:
            self.write_json(HTTPStatus.BAD_REQUEST, {"error": "file is required"})
            return
        suffix = Path(filename).suffix or ".wav"
        language = fields.get("language", "")

        temp_path = ""
        try:
            with tempfile.NamedTemporaryFile(delete=False, suffix=suffix) as handle:
                handle.write(file_bytes)
                temp_path = handle.name

            result = WORKER.transcribe(temp_path, language)
            response = {
                "text": result["text"],
                "language": result["language"],
                "duration": result["duration"],
                "model": fields.get("model", WORKER.model_name),
                "segments": result["segments"],
            }
            self.write_json(HTTPStatus.OK, response)
        except Exception as exc:  # noqa: BLE001
            logging.exception("transcription failed")
            self.write_json(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": str(exc)})
        finally:
            if temp_path:
                try:
                    os.remove(temp_path)
                except OSError:
                    pass

    def log_message(self, fmt: str, *args) -> None:
        logging.info("%s - %s", self.address_string(), fmt % args)

    def write_json(self, status: HTTPStatus, payload: dict) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status.value)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    server = ThreadingHTTPServer((WORKER.host, WORKER.port), Handler)
    logging.info("faster-whisper worker listening on http://%s:%s", WORKER.host, WORKER.port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logging.info("shutting down worker")
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
