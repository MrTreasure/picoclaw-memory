# Picoclaw Memory System

A lightweight, zero-dependency, single-binary AI Agent memory system. Built with **Go + SQLite FTS5**.

Drop-in replacement for PowerMem / Mem0 — no Python, no embedding models, no hundreds-MB venv.

## Why not Mem0 / PowerMem?

| Feature | Mem0 | PowerMem | **Picoclaw Memory** |
|---------|------|----------|-------------------|
| Runtime | Python 3.8+ | Python 3.11+ | **Go single binary** |
| Dependencies | 20+ packages | 30+ packages | **Zero** |
| Install size | ~500MB | ~800MB | **~8MB** |
| Embedding model | Required | Required | **Not needed** |
| Storage | Chroma/Redis | Chroma/Postgres | **SQLite (WAL + FTS5)** |
| Search | Vector + Hybrid | Vector + Hybrid | **FTS5 full-text + LIKE fallback** |
| Cold start | 5-10 min | 10-15 min | **Instant** |
| Chinese support | ❌ | ❌ | **FTS5 + LIKE dual engine** |

**Building million-doc production RAG pipelines?** → Use Mem0.  
**Just want your AI Agent to remember daily conversations?** → Use this.

## Features

- **Zero dependencies** — Pure Go, single binary, no runtime requirements
- **FTS5 full-text search** — Fast retrieval without embedding models
- **Chinese-friendly** — FTS5 with LIKE fallback for single-character tokenization
- **Daily auto-capture** — Extract facts, tasks, and notes from Daily Notes
- **Importance scoring** — `[imp:1-5]` markers for weighted retention
- **Periodic summarization** — Auto-condense raw entries into weekly/monthly summaries
- **Auto-forgetting** — Policy-based archival and deletion of stale memories
- **SQLite storage** — Single file, easy backup and migration

## Architecture

```
┌──────────────────────────────────────────┐
│               Pipeline                    │
│                                          │
│  📝 capture  ──→  📊 summarize  ──→  🗑️ forget │
│  (daily)          (weekly/monthly)  (auto-clean) │
│       │                │                  │
│       ▼                ▼                  │
│   SQLite DB  ←──────  FTS5  ←──────  recall    │
│   (raw)           (summary)        (search)    │
└──────────────────────────────────────────┘
```

## Quick Start

```bash
# Build all binaries
git clone https://github.com/MrTreasure/picoclaw-memory.git
cd picoclaw-memory

# Requires CGO (SQLite FTS5 needs C library)
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-capture ./cmd/capture/
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-recall ./cmd/recall/
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-summarize ./cmd/summarize/
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-forget ./cmd/forget/

# Install to PATH
cp pcm-capture pcm-recall pcm-summarize pcm-forget config.json /usr/local/bin/

# Capture today's memories
pcm-capture

# Search memories
pcm-recall "keywords"
```

## Usage

### 📝 capture — Daily Memory Capture

```bash
# Auto-detect today
pcm-capture

# Specific date
pcm-capture --date 2026-07-20
```

Extracts from `memory/YYYYMM/YYYYMMDD.md`:
- List items (`- text`) → default importance 3
- Tasks (`- [x] text` or under `## 今日任务`) → tagged as tasks
- With `[imp:N]` markers → custom importance (1-5)
- Deduplication: same date + same content won't be re-inserted

### 📊 summarize — Periodic Summarization

```bash
# Weekly summary (LLM mode)
pcm-summarize --period weekly

# Monthly summary (LLM mode)
pcm-summarize --period monthly

# Offline mode — no LLM, trigram Jaccard dedup + sorting (cheaper)
pcm-summarize --period weekly --offline
pcm-summarize --period monthly --offline
```

**Two modes:**

| Mode | Flag | Cost | How it works |
|------|------|------|-------------|
| Online (default) | *(none)* | LLM API fee | Calls LLM to generate condensed summaries |
| Offline | `--offline` | **$0** | Trigram Jaccard dedup + importance sorting + topic crop (top 3 per topic) |

**Offline algorithm:**
1. Group entries by topic
2. Deduplicate via trigram Jaccard similarity (threshold 0.6) — keeps higher-importance entry
3. Sort by importance desc → accessed_at desc
4. Crop to top 3 per topic
5. Write with imp=4 (weekly) or imp=6 (monthly)

Pipeline:
1. Reads unsummarized raw daily entries
2. Generates condensed summaries (LLM or offline)
3. Writes summary entries to DB (imp=4 weekly, imp=6 monthly)
4. Marks raw entries as archived

### 🔍 recall — Memory Search

```bash
# Full-text search
pcm-recall "keywords"

# Filter by topic
pcm-recall "AI" --topic notes

# Limit results
pcm-recall "LLM" --top 5

# JSON output
pcm-recall "agent" --json

# Include archived entries
pcm-recall "meeting" --archived

# Filter by minimum importance
pcm-recall "preferences" --importance 4
```

### 🗑️ forget — Auto-Forgetting

```bash
# Dry run (preview)
pcm-forget --dry-run

# Execute cleanup
pcm-forget
```

Forgetting rules:
- importance ≤ 2 + 7 days unaccessed → archive (`archived=1`)
- importance ≤ 1 + 14 days unaccessed → **delete permanently**
- Archived + 30 days unaccessed → **delete permanently**
- importance ≥ 3 entries: kept unless long-unaccessed

## Integration with AI Agent

### Architecture

Picoclaw Memory is the sole memory layer, paired with AGENT.md for session context:

```
┌──────────────────────────────┐
│      AGENT.md                │  ← always loaded at session start
│  family/device/security/DB   │
└──────────────────────────────┘
         │
         ▼
┌──────────────────────────────┐
│   SQLite FTS5 (memory.db)    │  ← on-demand search (pcm-recall)
│  daily capture + auto-forget  │
└──────────────────────────────┘
```

### Integration

As a Skill integration:

```yaml
# In SKILL.md
pcm-recall "user preferences"
```

Or via CLI directly:

```bash
# Get relevant memories as context
pcm-recall --top 5

# Search for preferences
pcm-recall "preferences" --importance 4

# Capture current conversation
pcm-capture --date $(date +%F)
```

## Configuration

Edit `config.json`:

```json
{
  "workspace": "/path/to/workspace",
  "memory_dir": "/path/to/workspace/memory",
  "db_path": "/path/to/workspace/memory/memory.db",
  "picoclaw_bin": "/path/to/picoclaw",
  "llm_model": "qwen3.7-plus",
  "retention_days": {
    "daily": 7,
    "weekly": 30,
    "monthly": 365
  }
}
```

| Field | Description | Default |
|-------|-------------|---------|
| workspace | Workspace root | `/path/to/workspace` |
| memory_dir | Daily Note directory | `{workspace}/memory` |
| db_path | SQLite database path | `{memory_dir}/memory.db` |
| picoclaw_bin | picoclaw CLI path (for LLM calls) | - |
| llm_model | Model for summarization | `qwen3.7-plus` |
| retention_days.daily | Daily entry retention | 7 |
| retention_days.weekly | Weekly summary retention | 30 |
| retention_days.monthly | Monthly summary retention | 365 |

## Project Structure

```
picoclaw-memory/
├── cmd/
│   ├── capture/main.go     # Daily memory capture
│   ├── recall/main.go      # Memory search
│   ├── summarize/main.go   # Periodic summarization
│   └── forget/main.go      # Memory forgetting & archival
├── internal/
│   ├── config/config.go    # Configuration loader
│   ├── db/db.go            # SQLite CRUD + FTS5 + forget logic
│   └── models/memory.go    # Data models
├── config.json             # Configuration file
├── config.json.example     # Configuration template
├── go.mod / go.sum
├── README.zh.md            # Chinese documentation
├── README.md               # English documentation (this file)
├── setup.sh                # Setup script
└── LICENSE                 # MIT
```

## Technical Details

### Why SQLite FTS5 over Embedding?

| Factor | FTS5 | Embedding |
|--------|------|-----------|
| Dependencies | ✅ CGO (build-time) | ❌ numpy + ONNX/Transformers |
| Deployment | ✅ Single file | ❌ Model download 500MB+ |
| Exact match | ✅ Supported | ❌ Fuzzy |
| Semantic search | ⚠️ Limited | ✅ Strong |
| Cold start | ✅ Instant | ❌ 5 minutes |
| Chinese tokenization | ⚠️ LIKE fallback | ✅ Good |

**Bottom line**: For agent conversation memory (hundreds to thousands of entries), FTS5 is enough. If semantic search is needed later, add embedding as a supplementary engine, not a replacement.

### Build Requirements

- Go 1.23+
- CGO_ENABLED=1 (SQLite FTS5 needs C library)
- Must use `-tags sqlite_fts5` flag when building

```bash
# Correct build
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-capture ./cmd/capture/
```

## License

MIT
