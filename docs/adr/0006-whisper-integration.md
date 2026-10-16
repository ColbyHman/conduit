# ADR 0006: Whisper Integration and Single-Machine Deployment

## Status
Accepted

## Context
Journal domain requires audio transcription. Whisper runs as a separate service (whisper-receiver or whisper.cpp server). Conduit's whisper-worker calls it via HTTP.

## Decision
**Whisper worker calls external Whisper API** via `WHISPER_URL`. Single-machine deployment uses `host.docker.internal` to reach host-local Whisper from Docker containers.

## Whisper API Contract
```
POST /transcribe
Content-Type: multipart/form-data
Body: file=@audio.m4a, model=medium, response_format=verbose_json
Response: { "text": "...", "segments": [...], "language": "en", "duration": 45.2 }
```
- Accepts `.m4a`, `.wav`, `.mp3`, `.webm`
- Returns `verbose_json` for word-level timestamps (future: clickable transcripts)
- Timeout: 5 minutes (configurable via `WHISPER_TIMEOUT`)

## Single-Machine Architecture
```
┌─────────────────────────────────────────────────────────────┐
│ Host Machine                                                │
│  ┌──────────────┐    ┌──────────────────────────────────┐  │
│  │ Whisper      │    │ Docker Compose Stack             │  │
│  │ (native,     │◄───│  ┌─────────┐ ┌────────┐ ┌──────┐ │  │
│  │  Metal GPU)  │    │  │ Postgres│ │Gateway │ │Worker│ │  │
│  │  :9000       │    │  │         │ │ :8080  │ │      │ │  │
│  └──────────────┘    │  └─────────┘ └────────┘ └──────┘ │  │
│                      │  ┌──────────────┐                  │  │
│                      │  │ WhisperWorker│                  │  │
│                      │  └──────────────┘                  │  │
│                      └──────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```
- Whisper runs **natively on host** (Metal GPU on Mac, CUDA on Linux)
- Docker containers reach it via `http://host.docker.internal:9000`
- No GPU passthrough needed — avoids Docker/Metal complexity

## Whisper Worker Flow
1. Poll `jobs` for `journal_transcribe` with `status = 'pending'`
2. Claim job (atomic UPDATE with `RETURNING`)
3. Download audio from `payload.audio_url` (presigned S3, local path, or base64)
4. POST to `WHISPER_URL/transcribe` with `model` from payload or default
5. On success: UPDATE job with `result = {text, segments, language}`, `status = 'completed'`
6. On failure: increment `attempt_count`, set `next_retry_at`, `status = 'pending'`

## Audio Source Options
| Source | `payload` Field | Notes |
|--------|-----------------|-------|
| Presigned S3 URL | `audio_url` | Preferred for production |
| Local file path | `audio_path` | Dev only; worker must have volume mount |
| Base64 inline | `audio_base64` | Small files only (<10MB) |

## Rationale
- **Decoupled** — Whisper can be upgraded/swapped without touching Conduit
- **GPU on host** — Best performance on Mac (Metal), Linux (CUDA); Docker GPU passthrough is brittle
- **Simple contract** — Standard multipart upload; compatible with whisper.cpp, faster-whisper, openai-whisper API
- **Single-machine dev** — `host.docker.internal` works on Docker Desktop (Mac/Win) and Linux (with `--add-host`)

## Consequences
- `WHISPER_URL` defaults to `http://host.docker.internal:9000` in docker-compose
- Whisper worker needs network access to host (default in Docker Desktop)
- Production: Whisper behind load balancer, `WHISPER_URL` points to internal DNS
- Audio download logic in worker (not gateway) — keeps gateway stateless