// Package db 提供 SQLite 数据库初始化与 CRUD 操作
package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/MrTreasure/picoclaw-memory/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

// InitDB 初始化 SQLite 数据库（WAL 模式 + FTS5 + 触发器）
func InitDB(dbPath string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// WAL 模式
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("set WAL: %w", err)
	}

	// 主表
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			content TEXT NOT NULL,
			topic TEXT DEFAULT 'general',
			source TEXT DEFAULT 'daily',
			importance INTEGER DEFAULT 3,
			created_at TEXT NOT NULL,
			accessed_at TEXT NOT NULL,
			access_count INTEGER DEFAULT 0,
			archived INTEGER DEFAULT 0,
			deleted INTEGER DEFAULT 0
		)
	`); err != nil {
		return nil, fmt.Errorf("create memories table: %w", err)
	}

	// 迁移：已有表添加 deleted 字段（忽略重复列错误）
	_, _ = conn.Exec("ALTER TABLE memories ADD COLUMN deleted INTEGER DEFAULT 0")

	// FTS5 全文搜索索引
	if _, err := conn.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			content, topic,
			content='memories',
			content_rowid='id'
		)
	`); err != nil {
		return nil, fmt.Errorf("create FTS5 table: %w", err)
	}

	// 触发器
	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content, topic)
			VALUES (new.id, new.content, new.topic);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, topic)
			VALUES('delete', old.id, old.content, old.topic);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, topic)
			VALUES('delete', old.id, old.content, old.topic);
			INSERT INTO memories_fts(rowid, content, topic)
			VALUES (new.id, new.content, new.topic);
		END`,
	}
	for _, t := range triggers {
		if _, err := conn.Exec(t); err != nil {
			return nil, fmt.Errorf("create trigger: %w", err)
		}
	}

	return conn, nil
}

// InsertMemory 插入一条记忆
func InsertMemory(conn *sql.DB, m *models.Memory) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if m.CreatedAt == "" {
		m.CreatedAt = now
	}
	if m.AccessedAt == "" {
		m.AccessedAt = now
	}
	if m.Topic == "" {
		m.Topic = "general"
	}
	if m.Source == "" {
		m.Source = "daily"
	}
	if m.Importance == 0 {
		m.Importance = 2
	}

	result, err := conn.Exec(
		`INSERT INTO memories (content, topic, source, importance, created_at, accessed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.Content, m.Topic, m.Source, m.Importance, m.CreatedAt, m.AccessedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert memory: %w", err)
	}
	return result.LastInsertId()
}

// scanMemory 扫描一行到 Memory 结构体
func scanMemory(row *sql.Row) (*models.Memory, error) {
	var m models.Memory
	var archived, deleted int
	err := row.Scan(&m.ID, &m.Content, &m.Topic, &m.Importance,
		&m.Source, &m.CreatedAt, &m.AccessedAt, &m.AccessCount, &archived, &deleted)
	if err != nil {
		return nil, err
	}
	m.Archived = archived != 0
	m.Deleted = deleted != 0
	return &m, nil
}

func scanMemories(rows *sql.Rows) ([]models.Memory, error) {
	var result []models.Memory
	for rows.Next() {
		var m models.Memory
		var archived, deleted int
		err := rows.Scan(&m.ID, &m.Content, &m.Topic, &m.Importance,
			&m.Source, &m.CreatedAt, &m.AccessedAt, &m.AccessCount, &archived, &deleted)
		if err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		m.Archived = archived != 0
		m.Deleted = deleted != 0
		result = append(result, m)
	}
	return result, rows.Err()
}

// freshnessScore 计算新鲜度加分（与 Python 版一致）
func freshnessScore(accessedAt string) float64 {
	if accessedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, accessedAt)
	if err != nil {
		return 0
	}
	days := time.Since(t).Hours() / 24
	switch {
	case days < 1:
		return 5
	case days < 7:
		return 3
	case days < 30:
		return 1
	default:
		return 0
	}
}

// accessScore 计算访问频次加分（与 Python 版一致）
func accessScore(count int) float64 {
	switch {
	case count > 10:
		return 3
	case count > 5:
		return 2
	case count > 2:
		return 1
	default:
		return 0
	}
}

// SearchMemories FTS5 全文搜索 + 排序
// 排序算法：rank + (importance * 1.5) + 新鲜度 + 访问频次
func SearchMemories(conn *sql.DB, query string, top int, topic string, importanceMin int, archived bool) ([]models.Memory, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	if strings.TrimSpace(query) == "" {
		return listAll(conn, top, topic, importanceMin, archived, now)
	}

	// 先尝试 FTS5 搜索
	results, err := searchFTS5(conn, query, top, topic, importanceMin, archived, now)
	if err != nil || len(results) == 0 {
		// FTS5 失败（中文单字符等）→ LIKE 兜底
		results, err = searchLike(conn, query, top, topic, importanceMin, archived, now)
	}
	if err != nil {
		return nil, err
	}

	// 更新访问记录
	updateAccessed(conn, results, now)

	return results, nil
}

// searchFTS5 使用 FTS5 全文搜索
func searchFTS5(conn *sql.DB, query string, top int, topic string, importanceMin int, archived bool, now string) ([]models.Memory, error) {
	var conditions []string
	var args []interface{}

	conditions = append(conditions, "memories_fts MATCH ?")
	args = append(args, query)

	if topic != "" {
		conditions = append(conditions, "m.topic = ?")
		args = append(args, topic)
	}
	if !archived {
		conditions = append(conditions, "m.archived = 0")
	}
	conditions = append(conditions, "m.deleted = 0")
	if importanceMin > 0 {
		conditions = append(conditions, "m.importance >= ?")
		args = append(args, importanceMin)
	}

	where := strings.Join(conditions, " AND ")
	args = append(args, top)

	sqlQuery := fmt.Sprintf(`
		SELECT m.id, m.content, m.topic, m.importance, m.source,
			   m.created_at, m.accessed_at, m.access_count, m.archived, m.deleted,
			   rank
		FROM memories_fts
		JOIN memories m ON m.id = memories_fts.rowid
		WHERE %s
		ORDER BY
			rank +
			(m.importance * 1.5) +
			CASE
				WHEN julianday(?) - julianday(m.accessed_at) < 1 THEN 5
				WHEN julianday(?) - julianday(m.accessed_at) < 7 THEN 3
				WHEN julianday(?) - julianday(m.accessed_at) < 30 THEN 1
				ELSE 0
			END +
			(CASE WHEN m.access_count > 10 THEN 3
				  WHEN m.access_count > 5 THEN 2
				  WHEN m.access_count > 2 THEN 1
				  ELSE 0 END)
		DESC
		LIMIT ?
	`, where)

	// 把 now 作为一个参数多次传入
	rows, err := conn.Query(sqlQuery, append(args, now, now, now)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Memory
	for rows.Next() {
		var m models.Memory
		var archived, deleted int
		var rank float64
		var source sql.NullString
		err := rows.Scan(&m.ID, &m.Content, &m.Topic, &m.Importance,
			&source, &m.CreatedAt, &m.AccessedAt, &m.AccessCount, &archived, &deleted, &rank)
		if err != nil {
			return nil, fmt.Errorf("scan FTS5 row: %w", err)
		}
		m.Archived = archived != 0
		m.Deleted = deleted != 0
		m.Source = source.String
		m.Rank = rank
		result = append(result, m)
	}
	return result, rows.Err()
}

// searchLike 使用 LIKE 兜底（处理中文分词问题）
func searchLike(conn *sql.DB, query string, top int, topic string, importanceMin int, archived bool, now string) ([]models.Memory, error) {
	var conditions []string
	var args []interface{}

	conditions = append(conditions, "m.content LIKE ?")
	args = append(args, "%"+query+"%")

	if topic != "" {
		conditions = append(conditions, "m.topic = ?")
		args = append(args, topic)
	}
	if !archived {
		conditions = append(conditions, "m.archived = 0")
	}
	conditions = append(conditions, "m.deleted = 0")
	if importanceMin > 0 {
		conditions = append(conditions, "m.importance >= ?")
		args = append(args, importanceMin)
	}

	where := strings.Join(conditions, " AND ")
	args = append(args, top)

	sqlQuery := fmt.Sprintf(`
		SELECT m.id, m.content, m.topic, m.importance, m.source,
			   m.created_at, m.accessed_at, m.access_count, m.archived, m.deleted
		FROM memories m
		WHERE %s
		ORDER BY m.importance DESC, m.accessed_at DESC
		LIMIT ?
	`, where)

	rows, err := conn.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Memory
	for rows.Next() {
		var m models.Memory
		var archived, deleted int
		var source sql.NullString
		err := rows.Scan(&m.ID, &m.Content, &m.Topic, &m.Importance,
			&source, &m.CreatedAt, &m.AccessedAt, &m.AccessCount, &archived, &deleted)
		if err != nil {
			return nil, fmt.Errorf("scan LIKE row: %w", err)
		}
		m.Archived = archived != 0
		m.Deleted = deleted != 0
		m.Source = source.String
		// LIKE 搜索的 rank 为 0
		m.Rank = 0
		result = append(result, m)
	}
	return result, rows.Err()
}

// listAll 列出所有记忆（无搜索关键词时调用）
func listAll(conn *sql.DB, top int, topic string, importanceMin int, archived bool, now string) ([]models.Memory, error) {
	var conditions []string
	var args []interface{}

	conditions = append(conditions, "1=1")

	if topic != "" {
		conditions = append(conditions, "m.topic = ?")
		args = append(args, topic)
	}
	if !archived {
		conditions = append(conditions, "m.archived = 0")
	}
	conditions = append(conditions, "m.deleted = 0")
	if importanceMin > 0 {
		conditions = append(conditions, "m.importance >= ?")
		args = append(args, importanceMin)
	}

	where := strings.Join(conditions, " AND ")
	args = append(args, top)

	sqlQuery := fmt.Sprintf(`
		SELECT m.id, m.content, m.topic, m.importance, m.source,
			   m.created_at, m.accessed_at, m.access_count, m.archived, m.deleted
		FROM memories m
		WHERE %s
		ORDER BY m.importance DESC, m.accessed_at DESC
		LIMIT ?
	`, where)

	rows, err := conn.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result, err := scanMemories(rows)
	if err != nil {
		return nil, err
	}

	// 更新访问记录
	updateAccessed(conn, result, now)

	return result, nil
}

// updateAccessed 更新记忆的访问时间和计数
func updateAccessed(conn *sql.DB, results []models.Memory, now string) {
	for _, m := range results {
		_, _ = conn.Exec(
			"UPDATE memories SET accessed_at = ?, access_count = access_count + 1 WHERE id = ?",
			now, m.ID,
		)
	}
}

// ListAll 公开的列表查询
func ListAll(conn *sql.DB, top int, topic string, importanceMin int, archived bool) ([]models.Memory, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	return listAll(conn, top, topic, importanceMin, archived, now)
}

// GetStats 获取记忆库统计信息
func GetStats(conn *sql.DB) (map[string]int, error) {
	stats := make(map[string]int)

	var total int
	err := conn.QueryRow("SELECT COUNT(*) FROM memories WHERE deleted=0").Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("get total: %w", err)
	}
	stats["total"] = total

	var archived int
	err = conn.QueryRow("SELECT COUNT(*) FROM memories WHERE archived=1 AND deleted=0").Scan(&archived)
	if err != nil {
		return nil, fmt.Errorf("get archived: %w", err)
	}
	stats["archived"] = archived

	var active int
	err = conn.QueryRow("SELECT COUNT(*) FROM memories WHERE archived=0 AND deleted=0").Scan(&active)
	if err != nil {
		return nil, fmt.Errorf("get active: %w", err)
	}
	stats["active"] = active

	// 按 topic 统计
	rows, err := conn.Query("SELECT topic, COUNT(*) FROM memories WHERE deleted=0 GROUP BY topic ORDER BY COUNT(*) DESC")
	if err != nil {
		return nil, fmt.Errorf("get topics: %w", err)
	}
	defer rows.Close()

	var topicCount int
	for rows.Next() {
		var topic string
		var cnt int
		if err := rows.Scan(&topic, &cnt); err != nil {
			return nil, err
		}
		stats["topic_"+topic] = cnt
		topicCount++
	}
	stats["topic_count"] = topicCount

	return stats, nil
}

// UpdateAccessedTime 更新单条记忆的访问时间
func UpdateAccessedTime(conn *sql.DB, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(
		"UPDATE memories SET accessed_at = ?, access_count = access_count + 1 WHERE id = ?",
		now, id,
	)
	return err
}

// ArchiveMemories 执行遗忘流程
// 归档规则:
//   - 重要性 ≤ 2 + 7天未访问 → archived=1
//   - 周报(importance=4) + 90天未访问 → archived=1
// 删除规则:
//   - 重要性 ≤ 1 + 14天未访问 → 删除
//   - 已归档且超过30天 → 删除
// 月报(importance=6)永久保留,不进入清理流程
func ArchiveMemories(conn *sql.DB, dryRun bool) (archived int, deleted int, skipped []models.Memory, err error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// 查询可归档的记忆: 低重要性 + 7天未访问
	archiveRows, err := conn.Query(
		`SELECT id, content, topic, importance, source, created_at, accessed_at, access_count, archived, deleted
		 FROM memories
		 WHERE archived = 0 AND deleted = 0
		   AND importance <= 2
		   AND julianday(?) - julianday(accessed_at) >= 7`,
		now,
	)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("query archivable (low imp): %w", err)
	}
	defer archiveRows.Close()

	archivable, err := scanMemories(archiveRows)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("scan archivable: %w", err)
	}

	for _, m := range archivable {
		if dryRun {
			skipped = append(skipped, m)
		} else {
			_, err := conn.Exec(
				"UPDATE memories SET archived = 1, accessed_at = ? WHERE id = ?",
				now, m.ID,
			)
			if err != nil {
				return archived, deleted, skipped, fmt.Errorf("archive %d: %w", m.ID, err)
			}
			archived++
		}
	}

	// 查询可归档的周报: 周报(imp=4) + 90天未访问
	weeklyRows, err := conn.Query(
		`SELECT id, content, topic, importance, source, created_at, accessed_at, access_count, archived, deleted
		 FROM memories
		 WHERE archived = 0 AND deleted = 0
		   AND importance = 4
		   AND julianday(?) - julianday(accessed_at) >= 90`,
		now,
	)
	if err != nil {
		return archived, deleted, skipped, fmt.Errorf("query archivable (weekly): %w", err)
	}
	defer weeklyRows.Close()

	weeklies, err := scanMemories(weeklyRows)
	if err != nil {
		return archived, deleted, skipped, fmt.Errorf("scan weekly: %w", err)
	}

	for _, m := range weeklies {
		if dryRun {
			skipped = append(skipped, m)
		} else {
			_, err := conn.Exec(
				"UPDATE memories SET archived = 1, accessed_at = ? WHERE id = ?",
				now, m.ID,
			)
			if err != nil {
				return archived, deleted, skipped, fmt.Errorf("archive weekly %d: %w", m.ID, err)
			}
			archived++
		}
	}

	// 查询可删除的记忆（软删除）
	// 1. 已归档且超过30天
	// 2. 重要性 ≤ 1 + 14天未访问
	deleteRows, err := conn.Query(
		`SELECT id, content, topic, importance, source, created_at, accessed_at, access_count, archived, deleted
		 FROM memories
		 WHERE deleted = 0
		   AND ((archived = 1 AND julianday(?) - julianday(accessed_at) >= 30)
		    OR (archived = 0 AND importance <= 1 AND julianday(?) - julianday(accessed_at) >= 14))`,
		now, now,
	)
	if err != nil {
		return archived, deleted, skipped, fmt.Errorf("query deletable: %w", err)
	}
	defer deleteRows.Close()

	deletable, err := scanMemories(deleteRows)
	if err != nil {
		return archived, deleted, skipped, fmt.Errorf("scan deletable: %w", err)
	}

	for _, m := range deletable {
		if dryRun {
			// 避免重复添加
			found := false
			for _, s := range skipped {
				if s.ID == m.ID {
					found = true
					break
				}
			}
			if !found {
				skipped = append(skipped, m)
			}
		} else {
			_, err := conn.Exec("UPDATE memories SET deleted = 1, accessed_at = ? WHERE id = ?", now, m.ID)
			if err != nil {
				return archived, deleted, skipped, fmt.Errorf("soft delete %d: %w", m.ID, err)
			}
			deleted++
		}
	}

	return archived, deleted, skipped, nil
}

// DeleteMemories 按 ID 列表软删除记忆
func DeleteMemories(conn *sql.DB, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = now
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	result, err := conn.Exec(
		fmt.Sprintf("UPDATE memories SET deleted = 1, accessed_at = ? WHERE id IN (%s)", strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("soft delete memories: %w", err)
	}
	return result.RowsAffected()
}

// GetAllMemories 获取所有记忆（不含排序，用于批量操作）
func GetAllMemories(conn *sql.DB) ([]models.Memory, error) {
	rows, err := conn.Query(
		`SELECT id, content, topic, importance, source, created_at, accessed_at, access_count, archived, deleted
		 FROM memories WHERE deleted = 0 ORDER BY id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// GetMemoryByContent 根据内容查找记忆（用于去重）
func GetMemoryByContent(conn *sql.DB, content string) (*models.Memory, error) {
	row := conn.QueryRow(
		`SELECT id, content, topic, importance, source, created_at, accessed_at, access_count, archived, deleted
		 FROM memories WHERE content = ?`, content,
	)
	return scanMemory(row)
}

// ContentExists 检查内容是否已存在
func ContentExists(conn *sql.DB, content string) (bool, error) {
	var count int
	err := conn.QueryRow("SELECT COUNT(*) FROM memories WHERE content = ?", content).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetMemoriesByDateRange 按时间范围和 source 查询记忆（用于总结）
func GetMemoriesByDateRange(conn *sql.DB, start, end time.Time, source string, archived bool) ([]models.Memory, error) {
	var conditions []string
	var args []interface{}

	conditions = append(conditions, "created_at >= ? AND created_at <= ?")
	args = append(args, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))

	if source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, source)
	}

	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	conditions = append(conditions, "archived = ?")
	args = append(args, archivedInt)
	conditions = append(conditions, "deleted = 0")

	where := strings.Join(conditions, " AND ")

	rows, err := conn.Query(fmt.Sprintf(`
		SELECT id, content, topic, importance, source, created_at, accessed_at, access_count, archived, deleted
		FROM memories WHERE %s ORDER BY created_at ASC`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("query date range: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

// CountBySourceAndTopic 按 source + topic 统计数量（用于检查是否已总结）
func CountBySourceAndTopic(conn *sql.DB, source, topic string) (int, error) {
	var count int
	err := conn.QueryRow(
		"SELECT COUNT(*) FROM memories WHERE source = ? AND topic = ?",
		source, topic,
	).Scan(&count)
	return count, err
}

// ArchiveByIDs 按 ID 列表归档记忆
func ArchiveByIDs(conn *sql.DB, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = now
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	result, err := conn.Exec(
		fmt.Sprintf("UPDATE memories SET archived = 1, accessed_at = ? WHERE id IN (%s)",
			strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("archive by ids: %w", err)
	}
	return result.RowsAffected()
}

// PurgeDeletedMemories 物理删除所有已软删除的记忆（--purge 用）
func PurgeDeletedMemories(conn *sql.DB) (int64, error) {
	result, err := conn.Exec("DELETE FROM memories WHERE deleted = 1")
	if err != nil {
		return 0, fmt.Errorf("purge deleted: %w", err)
	}
	return result.RowsAffected()
}
