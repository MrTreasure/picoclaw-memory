#!/usr/bin/env python3
"""
capture.py — 每日总结记忆采集
从 Daily Note 中提取事实写入 SQLite，支持 [imp:N] 标记
零外部依赖，只用 Python 3.11 内置库
"""

import re
import sys
import json
import hashlib
from datetime import date, datetime, timezone
from pathlib import Path

# 从 db.py 借用
sys.path.insert(0, str(Path(__file__).parent))
from db import load_config, init_db

# ── 配置 ──────────────────────────────────────────
cfg = load_config()
MEMORY_DIR = Path(cfg["memory_dir"])
DB_PATH = Path(cfg["db_path"])

# ── 工具函数 ──────────────────────────────────────


def get_today_date():
    """返回今天的日期字符串 YYYY-MM-DD"""
    # 支持 --date 参数覆写
    for i, arg in enumerate(sys.argv):
        if arg == "--date" and i + 1 < len(sys.argv):
            return sys.argv[i + 1]
    return date.today().isoformat()


def find_daily_note(dt_str):
    """查找指定日期的 daily note 文件"""
    dt = date.fromisoformat(dt_str)
    yearmonth = dt.strftime("%Y%m")
    filename = dt.strftime("%Y%m%d.md")
    # 可能的路径
    candidates = [
        MEMORY_DIR / yearmonth / filename,
        MEMORY_DIR / "daily" / yearmonth / filename,
        MEMORY_DIR / filename,
    ]
    for p in candidates:
        if p.exists():
            return p
    return None


def parse_daily_note(content):
    """解析 daily note，提取记忆条目
    
    提取规则:
    1. [imp:N] 标记的行 → 重要性 N
    2. 无标记的列表项（- / *）→ 重要性 3（默认）
    3. 标题行 (## xxx) → 作为 topic 上下文
    """
    entries = []
    current_topic = "general"

    for line in content.split("\n"):
        line = line.strip()
        if not line:
            continue

        # 跳过一级标题（# 文档标题）
        if re.match(r"^#\s", line) and not re.match(r"^##", line):
            continue

        # 检测二级及以上标题作为 topic
        heading_match = re.match(r"^#{2,4}\s+(.+)", line)
        if heading_match:
            heading_text = heading_match.group(1).lower()
            # 映射常用标题到 topic
            if "备忘" in heading_text or "note" in heading_text:
                current_topic = "notes"
            elif "任务" in heading_text or "todo" in heading_text or "task" in heading_text:
                current_topic = "tasks"
            elif "学习" in heading_text or "study" in heading_text or "learn" in heading_text:
                current_topic = "learning"
            else:
                current_topic = heading_text.strip(" #").replace(" ", "-").lower()
            continue

        # 提取 [imp:N] 标记
        imp_match = re.search(r"\[imp:(\d)\]", line)
        importance = int(imp_match.group(1)) if imp_match else 3
        # 清除标记后的内容
        clean_line = re.sub(r"\s*\[imp:\d\]", "", line).strip()
        # 清除列表标记
        clean_line = re.sub(r"^[-*]\s+", "", clean_line).strip()
        # 清除勾选框
        clean_line = re.sub(r"^-\s*\[[ x]\]\s+", "", clean_line).strip()

        # 跳过空行、纯标记、任务标题
        if not clean_line:
            continue
        if re.match(r"^##", clean_line):
            continue

        entries.append({
            "content": clean_line,
            "topic": current_topic,
            "importance": importance,
        })

    return entries


def deduplicate(entries, conn):
    """基于内容 hash 去重，避免重复插入"""
    existing = set()
    try:
        rows = conn.execute("SELECT content FROM memories").fetchall()
        existing = set(r[0] for r in rows)
    except Exception:
        pass

    unique = []
    for e in entries:
        if e["content"] not in existing:
            unique.append(e)
            existing.add(e["content"])
    return unique


def save_entries(entries, source, conn):
    """写入记忆到 SQLite"""
    now = datetime.now(timezone.utc).isoformat()
    count = 0
    for e in entries:
        conn.execute(
            """INSERT INTO memories (content, topic, source, importance, created_at, accessed_at)
               VALUES (?, ?, ?, ?, ?, ?)""",
            (e["content"], e["topic"], source, e["importance"], now, now),
        )
        count += 1
    conn.commit()
    return count


# ── 主流程 ────────────────────────────────────────


def main():
    dt_str = get_today_date()
    print(f"📝 Capture: {dt_str}")

    conn = init_db(DB_PATH)

    # 1. 尝试从 daily note 提取
    note_path = find_daily_note(dt_str)
    entries = []

    if note_path:
        content = note_path.read_text(encoding="utf-8")
        parsed = parse_daily_note(content)
        # 去重
        entries = deduplicate(parsed, conn)
        source = "daily"
        print(f"   📄 从 {note_path.name} 解析出 {len(parsed)} 条，新增 {len(entries)} 条")
    else:
        print(f"   ⚠️  未找到 {dt_str} 的 daily note，跳过")
        source = "manual"

    # 2. 写入数据库
    if entries:
        saved = save_entries(entries, source, conn)
        print(f"   ✅ 写入 {saved} 条记忆")
    else:
        print(f"   ℹ️  无新记忆")

    # 3. 统计
    total = conn.execute("SELECT COUNT(*) FROM memories").fetchone()[0]
    by_topic = conn.execute(
        "SELECT topic, COUNT(*) FROM memories GROUP BY topic ORDER BY COUNT(*) DESC"
    ).fetchall()
    print(f"\n📊 记忆库总计: {total} 条")
    for topic, cnt in by_topic:
        print(f"   {topic}: {cnt} 条")

    conn.close()


if __name__ == "__main__":
    main()
