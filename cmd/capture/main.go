// Command capture — 每日总结记忆采集
// 从 Daily Note 中提取事实写入 SQLite，支持 [imp:N] 标记
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MrTreasure/picoclaw-memory/internal/config"
	"github.com/MrTreasure/picoclaw-memory/internal/db"
	"github.com/MrTreasure/picoclaw-memory/internal/models"
)

// parseEntry 解析后的记忆条目
type parseEntry struct {
	Content    string
	Topic      string
	Importance int
}

func main() {
	dateStr := flag.String("date", "", "日期 (YYYY-MM-DD)，默认今天")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 确定日期
	dt := *dateStr
	if dt == "" {
		dt = time.Now().Format("2006-01-02")
	}

	fmt.Printf("📝 Capture: %s\n", dt)

	// 初始化数据库
	conn, err := db.InitDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 数据库初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// 查找 daily note
	notePath := findDailyNote(cfg.MemoryDir, dt)
	var entries []parseEntry

	if notePath != "" {
		data, err := os.ReadFile(notePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  读取 %s 失败: %v\n", notePath, err)
		} else {
			parsed := parseDailyNote(string(data))
			// 去重
			entries = deduplicate(parsed, conn)
			fmt.Printf("   📄 从 %s 解析出 %d 条，新增 %d 条\n",
				filepath.Base(notePath), len(parsed), len(entries))
		}
	} else {
		fmt.Printf("   ⚠️  未找到 %s 的 daily note，跳过\n", dt)
	}

	// 写入数据库
	if len(entries) > 0 {
		saved := saveEntries(entries, "daily", conn)
		fmt.Printf("   ✅ 写入 %d 条记忆\n", saved)
	} else {
		fmt.Printf("   ℹ️  无新记忆\n")
	}

	// 统计
	stats, err := db.GetStats(conn)
	if err == nil {
		fmt.Printf("\n📊 记忆库总计: %d 条\n", stats["total"])
		// 按 topic 统计
		for k, v := range stats {
			if strings.HasPrefix(k, "topic_") && k != "topic_count" {
				fmt.Printf("   %s: %d 条\n", strings.TrimPrefix(k, "topic_"), v)
			}
		}
	}
}

// findDailyNote 查找指定日期的 daily note 文件
func findDailyNote(memoryDir, dt string) string {
	t, err := time.Parse("2006-01-02", dt)
	if err != nil {
		return ""
	}
	yearMonth := t.Format("200601")
	filename := t.Format("20060102") + ".md"

	candidates := []string{
		filepath.Join(memoryDir, yearMonth, filename),
		filepath.Join(memoryDir, "daily", yearMonth, filename),
		filepath.Join(memoryDir, filename),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// parseDailyNote 解析 daily note，提取记忆条目
//
// 提取规则:
// 1. [imp:N] 标记的行 → 重要性 N
// 2. 无标记的列表项（- / *）→ 重要性 3（默认）
// 3. 标题行 (## xxx) → 作为 topic 上下文
func parseDailyNote(content string) []parseEntry {
	var entries []parseEntry
	currentTopic := "general"

	// 编译正则
	headingRe := regexp.MustCompile(`^#{2,4}\s+(.+)`)
	impRe := regexp.MustCompile(`\[imp:(\d)\]`)
	cleanLineRe := regexp.MustCompile(`\s*\[imp:\d\]`)
	listMarkerRe := regexp.MustCompile(`^[-*]\s+`)
	taskMarkerRe := regexp.MustCompile(`^-\s*\[[ x]\]\s+`)
	headingOnlyRe := regexp.MustCompile(`^##`)

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 跳过一级标题
		if strings.HasPrefix(line, "# ") {
			continue
		}

		// 检测二级及以上标题作为 topic
		if match := headingRe.FindStringSubmatch(line); match != nil {
			headingText := strings.ToLower(match[1])
			switch {
			case strings.Contains(headingText, "备忘") || strings.Contains(headingText, "note"):
				currentTopic = "notes"
			case strings.Contains(headingText, "任务") || strings.Contains(headingText, "todo") ||
				strings.Contains(headingText, "task"):
				currentTopic = "tasks"
			case strings.Contains(headingText, "学习") || strings.Contains(headingText, "study") ||
				strings.Contains(headingText, "learn"):
				currentTopic = "learning"
			default:
				currentTopic = strings.NewReplacer(" ", "-").Replace(strings.Trim(headingText, " #"))
			}
			continue
		}

		// 提取 [imp:N] 标记
		importance := 2
		if impMatch := impRe.FindStringSubmatch(line); impMatch != nil {
			fmt.Sscanf(impMatch[1], "%d", &importance)
		}

		// 清除标记
		clean := cleanLineRe.ReplaceAllString(line, "")
		// 清除列表标记
		clean = listMarkerRe.ReplaceAllString(clean, "")
		// 清除勾选框
		clean = taskMarkerRe.ReplaceAllString(clean, "")
		clean = strings.TrimSpace(clean)

		// 跳过空行、纯标记、任务标题
		if clean == "" || headingOnlyRe.MatchString(clean) {
			continue
		}

		entries = append(entries, parseEntry{
			Content:    clean,
			Topic:      currentTopic,
			Importance: importance,
		})
	}

	return entries
}

// deduplicate 基于内容去重，避免重复插入
func deduplicate(entries []parseEntry, conn *sql.DB) []parseEntry {
	var unique []parseEntry
	for _, e := range entries {
		exists, err := db.ContentExists(conn, e.Content)
		if err != nil || !exists {
			unique = append(unique, e)
		}
	}
	return unique
}

// saveEntries 写入记忆到 SQLite
func saveEntries(entries []parseEntry, source string, conn *sql.DB) int {
	count := 0
	for _, e := range entries {
		m := &models.Memory{
			Content:    e.Content,
			Topic:      e.Topic,
			Source:     source,
			Importance: e.Importance,
		}
		_, err := db.InsertMemory(conn, m)
		if err == nil {
			count++
		}
	}
	return count
}
