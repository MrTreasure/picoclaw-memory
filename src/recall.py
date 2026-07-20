#!/usr/bin/env python3
"""
recall.py — 按需记忆检索
使用 SQLite FTS5 全文搜索，按重要性和新鲜度排序
零外部依赖，只用 Python 3.11 内置库
"""

import sys
import json
import sqlite3
import argparse
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from db import load_config, init_db

# ── 配置 ──
cfg = load_config()
DB_PATH = Path(cfg["db_path"])


def search(query, top=10, topic=None, importance_min=None, archived=False):
    """FTS5 搜索 + 排序
    
    排序算法:
    - FTS5 内置 rank（BM25）
    - 重要性加权 (importance * 1.5)
    - 新鲜度加权 (最近访问加分)
    - access_count 加权 (高访问 = 高价值)
    """
    conn = init_db(DB_PATH)

    # 空查询：直接查 memories 表，不用 FTS
    if not query.strip():
        sql = """SELECT m.id, m.content, m.topic, m.importance, m.source,
                        m.created_at, m.accessed_at, m.access_count, 0 as rank
                 FROM memories m
                 WHERE 1=1"""
        params = []
        if topic:
            sql += " AND m.topic = ?"
            params.append(topic)
        if not archived:
            sql += " AND m.archived = 0"
        sql += " ORDER BY m.importance DESC, m.accessed_at DESC LIMIT ?"
        params.append(top)
        rows = conn.execute(sql, params).fetchall()

        # 更新访问记录
        for row in rows:
            conn.execute(
                "UPDATE memories SET accessed_at = ?, access_count = access_count + 1 WHERE id = ?",
                (datetime.now(timezone.utc).isoformat(), row[0]),
            )
        conn.commit()

        results = []
        for row in rows:
            results.append({
                "id": row[0],
                "content": row[1],
                "topic": row[2],
                "importance": row[3],
                "source": row[4],
                "created_at": row[5],
                "accessed_at": row[6],
                "access_count": row[7],
                "rank": 0,
            })
        conn.close()
        return results

    # 先试试 FTS5 全文搜索
    try:
        conditions = ["memories_fts MATCH ?"]
        params = [query]

        if topic:
            conditions.append("m.topic = ?")
            params.append(topic)

        if not archived:
            conditions.append("m.archived = 0")

        if importance_min:
            conditions.append("m.importance >= ?")
            params.append(importance_min)

        where = " AND ".join(conditions)

        sql = f"""
            SELECT m.id, m.content, m.topic, m.importance, m.source,
                   m.created_at, m.accessed_at, m.access_count,
                   rank
            FROM memories_fts
            JOIN memories m ON m.id = memories_fts.rowid
            WHERE {where}
            ORDER BY 
                rank + 
                (m.importance * 1.5) + 
                CASE 
                    WHEN julianday('now') - julianday(m.accessed_at) < 1 THEN 5
                    WHEN julianday('now') - julianday(m.accessed_at) < 7 THEN 3
                    WHEN julianday('now') - julianday(m.accessed_at) < 30 THEN 1
                    ELSE 0
                END +
                (CASE WHEN m.access_count > 10 THEN 3 
                      WHEN m.access_count > 5 THEN 2
                      WHEN m.access_count > 2 THEN 1 
                      ELSE 0 END)
            DESC
            LIMIT ?
        """
        params.append(top)

        rows = conn.execute(sql, params).fetchall()
    except sqlite3.OperationalError:
        # FTS5 分词失败（常见于中文单个字符查询），兜底用 LIKE
        rows = []

    # FTS5 没命中的话，用 LIKE 兜底（处理中文分词问题）
    if not rows:
        conditions = ["m.content LIKE ?"]
        like_params = [f"%{query}%"]

        if topic:
            conditions.append("m.topic = ?")
            like_params.append(topic)

        if not archived:
            conditions.append("m.archived = 0")

        if importance_min:
            conditions.append("m.importance >= ?")
            like_params.append(importance_min)

        where = " AND ".join(conditions)
        like_sql = f"""
            SELECT m.id, m.content, m.topic, m.importance, m.source,
                   m.created_at, m.accessed_at, m.access_count, 0 as rank
            FROM memories m
            WHERE {where}
            ORDER BY m.importance DESC, m.accessed_at DESC
            LIMIT ?
        """
        like_params.append(top)
        rows = conn.execute(like_sql, like_params).fetchall()

    # 更新访问记录
    for row in rows:
        conn.execute(
            "UPDATE memories SET accessed_at = ?, access_count = access_count + 1 WHERE id = ?",
            (datetime.now(timezone.utc).isoformat(), row[0]),
        )
    conn.commit()

    results = []
    for row in rows:
        results.append({
            "id": row[0],
            "content": row[1],
            "topic": row[2],
            "importance": row[3],
            "source": row[4],
            "created_at": row[5],
            "accessed_at": row[6],
            "access_count": row[7],
            "rank": round(row[8], 2),
        })

    conn.close()
    return results


def format_results(results, query):
    """格式化输出"""
    if not results:
        return f"🔍 未找到与「{query}」相关的记忆"

    lines = [f"🔍 搜索「{query}」— 找到 {len(results)} 条结果\n"]
    for i, r in enumerate(results, 1):
        imp_stars = "⭐" * r["importance"]
        lines.append(f"{i}. [{r['topic']}] {r['content']}")
        lines.append(f"   重要性: {imp_stars}  来源: {r['source']}  访问: {r['access_count']}次")
        lines.append(f"   FTS 分: {r['rank']}")
        lines.append("")
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description="检索记忆库")
    parser.add_argument("query", nargs="?", help="搜索关键词")
    parser.add_argument("--top", type=int, default=10, help="返回条数 (默认 10)")
    parser.add_argument("--topic", help="按 topic 过滤")
    parser.add_argument("--importance", type=int, help="最低重要性 (1-5)")
    parser.add_argument("--archived", action="store_true", help="包含已归档条目")
    parser.add_argument("--json", action="store_true", help="JSON 格式输出")

    args = parser.parse_args()

    if not args.query:
        # 无查询：显示最近记忆
        results = search("", top=args.top, topic=args.topic,
                         importance_min=args.importance, archived=args.archived)
    else:
        results = search(args.query, top=args.top, topic=args.topic,
                         importance_min=args.importance, archived=args.archived)

    if args.json:
        print(json.dumps(results, ensure_ascii=False, indent=2))
    else:
        print(format_results(results, args.query or "全部"))


if __name__ == "__main__":
    main()
