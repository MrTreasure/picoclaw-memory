#!/usr/bin/env python3
"""
db.py — 共享数据库初始化模块
零外部依赖，只用 Python 内置库
"""

import sqlite3
import json
import os
from pathlib import Path

# 默认配置路径
DEFAULT_CONFIG = Path(__file__).parent / "config.json"


def load_config(config_path=None):
    """加载配置文件"""
    path = Path(config_path) if config_path else DEFAULT_CONFIG
    if not path.exists():
        raise FileNotFoundError(f"Config not found: {path}")
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def init_db(db_path):
    """初始化 SQLite 数据库 + FTS5 索引"""
    db_path = Path(db_path)
    db_path.parent.mkdir(parents=True, exist_ok=True)

    conn = sqlite3.connect(str(db_path))
    conn.execute("PRAGMA journal_mode=WAL")

    # 主表
    conn.execute("""
        CREATE TABLE IF NOT EXISTS memories (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            content TEXT NOT NULL,
            topic TEXT DEFAULT 'general',
            source TEXT DEFAULT 'daily',
            importance INTEGER DEFAULT 3,
            created_at TEXT NOT NULL,
            accessed_at TEXT NOT NULL,
            access_count INTEGER DEFAULT 0,
            archived INTEGER DEFAULT 0
        )
    """)

    # FTS5 全文搜索索引
    conn.execute("""
        CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
            content, topic,
            content='memories',
            content_rowid='id'
        )
    """)

    # 触发器：自动同步 FTS
    triggers = [
        """CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
            INSERT INTO memories_fts(rowid, content, topic)
            VALUES (new.id, new.content, new.topic);
        END""",
        """CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
            INSERT INTO memories_fts(memories_fts, rowid, content, topic)
            VALUES('delete', old.id, old.content, old.topic);
        END""",
        """CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
            INSERT INTO memories_fts(memories_fts, rowid, content, topic)
            VALUES('delete', old.id, old.content, old.topic);
            INSERT INTO memories_fts(rowid, content, topic)
            VALUES (new.id, new.content, new.topic);
        END""",
    ]

    for trigger in triggers:
        conn.execute(trigger)

    conn.commit()
    return conn


if __name__ == "__main__":
    cfg = load_config()
    conn = init_db(cfg["db_path"])
    print(f"[OK] Database initialized at {cfg['db_path']}")

    # 验证 FTS5
    tables = conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table'"
    ).fetchall()
    print(f"[OK] Tables: {[t[0] for t in tables]}")
    conn.close()
