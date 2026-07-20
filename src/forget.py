#!/usr/bin/env python3
"""
forget.py — 记忆遗忘与归档
- 重要性 ≤ 2 + 7天未访问 → archived=1（归档）
- 重要性 ≤ 1 + 14天未访问 → 删除
零外部依赖，只用 Python 3.11 内置库
"""

import sys
from datetime import datetime, timezone, timedelta
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from db import load_config, init_db

# ── 配置 ──
cfg = load_config()
DB_PATH = Path(cfg["db_path"])

# ── 遗忘规则 ──
ARCHIVE_DAYS = 7       # 重要性 ≤ 2 + N 天未访问 → 归档
ARCHIVE_IMPORTANCE = 2
DELETE_DAYS = 14       # 重要性 ≤ 1 + N 天未访问 → 删除
DELETE_IMPORTANCE = 1


def forget(conn, dry_run=False):
    """执行遗忘流程"""
    now = datetime.now(timezone.utc)
    actions = {"archived": [], "deleted": [], "skipped": []}

    # 查询所有可归档的记忆
    archivable = conn.execute(
        """SELECT id, content, topic, importance, accessed_at
           FROM memories
           WHERE archived = 0
             AND importance <= ?
             AND julianday(?) - julianday(accessed_at) >= ?
        """,
        (ARCHIVE_IMPORTANCE, now.isoformat(), ARCHIVE_DAYS),
    ).fetchall()

    for row in archivable:
        archived_days = (now - datetime.fromisoformat(row[4])).days if row[4] else 999
        if dry_run:
            actions["skipped"].append(row)
        else:
            conn.execute(
                "UPDATE memories SET archived = 1, accessed_at = ? WHERE id = ?",
                (now.isoformat(), row[0]),
            )
            actions["archived"].append(row)

    # 查询所有可删除的记忆（已归档 + 超过删除期限）
    # 包括：重要性 ≤ 1 且超过 14 天
    # 以及已归档且超过 30 天
    deletable = conn.execute(
        """SELECT id, content, topic, importance, accessed_at, archived
           FROM memories
           WHERE (archived = 1 AND julianday(?) - julianday(accessed_at) >= 30)
              OR (archived = 0 AND importance <= ? AND julianday(?) - julianday(accessed_at) >= ?)
        """,
        (now.isoformat(), DELETE_IMPORTANCE, now.isoformat(), DELETE_DAYS),
    ).fetchall()

    for row in deletable:
        if dry_run:
            if row not in actions["skipped"]:
                actions["skipped"].append(row)
        else:
            conn.execute("DELETE FROM memories WHERE id = ?", (row[0],))
            actions["deleted"].append(row)

    if not dry_run:
        conn.commit()

    return actions, conn


def format_report(actions, conn, dry_run=False):
    """格式化清理报告"""
    lines = []
    mode = "🔍 模拟运行" if dry_run else "🗑️  执行清理"
    lines.append(f"=== {mode} ===")
    lines.append(f"时间: {datetime.now().strftime('%Y-%m-%d %H:%M')}\n")

    if actions["archived"]:
        lines.append(f"📦 归档 ({len(actions['archived'])} 条):")
        for a in actions["archived"][:10]:
            lines.append(f"   [{a[2]}] {a[1][:60]}")
        if len(actions["archived"]) > 10:
            lines.append(f"   ... 还有 {len(actions['archived']) - 10} 条")
        lines.append("")

    if actions["deleted"]:
        lines.append(f"🗑️  删除 ({len(actions['deleted'])} 条):")
        for d in actions["deleted"][:10]:
            lines.append(f"   [{d[2]}] {d[1][:60]}")
        if len(actions["deleted"]) > 10:
            lines.append(f"   ... 还有 {len(actions['deleted']) - 10} 条")
        lines.append("")

    if actions["skipped"] and dry_run:
        lines.append(f"💤 将被处理 ({len(actions['skipped'])} 条):")
        for s in actions["skipped"][:5]:
            lines.append(f"   [{s[2]}] {s[1][:60]}")
        lines.append("")

    if not any(actions.values()):
        lines.append("ℹ️  没有需要清理的记忆")

    # 统计
    total = conn.execute("SELECT COUNT(*) FROM memories").fetchone()[0]
    archived = conn.execute("SELECT COUNT(*) FROM memories WHERE archived=1").fetchone()[0]
    active = total - archived
    lines.append(f"\n📊 当前状态:")
    lines.append(f"   总计: {total} 条 | 活跃: {active} 条 | 已归档: {archived} 条")

    return "\n".join(lines)


def main():
    import argparse

    parser = argparse.ArgumentParser(description="记忆遗忘与归档")
    parser.add_argument("--dry-run", action="store_true", help="模拟运行，不实际修改")
    args = parser.parse_args()

    conn = init_db(DB_PATH)
    actions, conn = forget(conn, dry_run=args.dry_run)
    report = format_report(actions, conn, dry_run=args.dry_run)
    print(report)
    conn.close()


if __name__ == "__main__":
    main()
