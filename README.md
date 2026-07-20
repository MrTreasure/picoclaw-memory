# Picoclaw Memory System

A lightweight, zero-dependency memory system for AI agents. Replaces PowerMem/Mem0 with pure Python 3.11 built-in libraries — no numpy, no sentence-transformers, no external dependencies.

## Why not Mem0 / PowerMem?

| Feature | Mem0 | PowerMem | **Picoclaw Memory** |
|---------|------|----------|-------------------|
| Dependencies | 20+ packages | 30+ packages | **zero** |
| Install size | ~500MB | ~800MB | **~20KB** |
| Embedding model | Required | Required | **not needed** |
| Storage | Chroma/Redis | Chroma/Postgres | **SQLite** |
| Search | Vector + hybrid | Vector + hybrid | **FTS5 full-text** |
| Cold start | 5-10 min | 10-15 min | **instant** |

**If you're building a production RAG pipeline with million-scale documents**, use Mem0.  
**If you just want your AI agent to remember stuff from daily conversations**, use this.

## Features

- **Zero external dependencies** — only Python 3.11+ built-in modules (`sqlite3`, `json`, `re`, `datetime`, `pathlib`)
- **Daily Capture** — automatically extract facts from daily notes
- **FTS5 Full-Text Search** — fast retrieval without embedding models
- **Importance Scoring** — tag memories with `[imp:1-5]` markers
- **Automatic Forgetting** — archive or delete stale memories by policy
- **SQLite Storage** — single file, easy backup and portability

## Quick Start

```bash
# Clone
git clone https://github.com/MrTreasure/picoclaw-memory.git
cd picoclaw-memory

# Initialize (no pip install needed!)
python3 src/db.py

# Capture today's memories from daily note
python3 src/capture.py --date 2026-07-20

# Search
python3 src/recall.py "AI agent"
python3 src/recall.py "" --top 20    # list recent

# Clean up old memories
python3 src/forget.py --dry-run       # preview
python3 src/forget.py                 # execute
```

Or use the install script:

```bash
bash setup.sh
```

## Usage

### Capture daily memories

```bash
# Auto-detect today's date
python3 src/capture.py

# Specify a date
python3 src/capture.py --date 2026-07-20
```

Capture reads daily notes from `memory/YYYYMM/YYYYMMDD.md` and extracts:
- Bullet list items (`- 内容`) → importance 3
- Tasks (`- [x] 内容`) → tagged as `tasks`
- Items with `[imp:N]` marker → custom importance

### Search memories

```bash
# Full-text search
python3 src/recall.py "keyword"

# Filter by topic
python3 src/recall.py "AI" --topic notes

# Limit results
python3 src/recall.py "LLM" --top 5

# JSON output
python3 src/recall.py "agent" --json
```

### Memory maintenance

```bash
# Preview cleanup
python3 src/forget.py --dry-run

# Execute
python3 src/forget.py
```

Forgetting rules:
- Importance ≤ 2 + 7 days no access → archived
- Importance ≤ 1 + 14 days no access → deleted

## Project Structure

```
picoclaw-memory/
├── src/
│   ├── db.py         # Database initialization & schema
│   ├── capture.py    # Daily memory capture
│   ├── recall.py     # Memory search & retrieval
│   └── forget.py     # Memory forgetting & archiving
├── config.json       # Configuration template
├── setup.sh          # Setup script
├── LICENSE           # MIT
├── README.md         # This file
└── .gitignore
```

## Configuration

Edit `config.json`:

```json
{
  "memory_dir": "/path/to/memory",
  "db_path": "/path/to/memory.db"
}
```

## Integration with AI Agents

Add to your agent's workflow:

```python
import subprocess

# capture daily
subprocess.run(["python3", "capture.py"])

# query
result = subprocess.run(
    ["python3", "recall.py", "user preference", "--json"],
    capture_output=True, text=True
)
```

## License

MIT
