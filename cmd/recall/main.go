// Command recall — 按需记忆检索
// 使用 SQLite FTS5 全文搜索，按重要性和新鲜度排序
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/MrTreasure/picoclaw-memory/internal/config"
	"github.com/MrTreasure/picoclaw-memory/internal/db"
	"github.com/MrTreasure/picoclaw-memory/internal/models"
)

func main() {
	top := flag.Int("top", 10, "返回条数 (默认 10)")
	topic := flag.String("topic", "", "按 topic 过滤")
	importance := flag.Int("importance", 0, "最低重要性 (1-5)")
	archived := flag.Bool("archived", false, "包含已归档条目")
	jsonOut := flag.Bool("json", false, "JSON 格式输出")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化数据库
	conn, err := db.InitDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 数据库初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// 获取查询参数
	query := strings.Join(flag.Args(), " ")

	var results []models.Memory
	if query == "" {
		// 无查询：显示最近记忆
		results, err = db.ListAll(conn, *top, *topic, *importance, *archived)
	} else {
		results, err = db.SearchMemories(conn, query, *top, *topic, *importance, *archived)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 搜索失败: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		outputJSON(results)
	} else {
		displayName := query
		if query == "" {
			displayName = "全部"
		}
		fmt.Print(formatResults(results, displayName))
	}
}

func outputJSON(results []models.Memory) {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ JSON 序列化失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func formatResults(results []models.Memory, query string) string {
	if len(results) == 0 {
		return fmt.Sprintf("🔍 未找到与「%s」相关的记忆\n", query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🔍 搜索「%s」— 找到 %d 条结果\n\n", query, len(results))

	for i, r := range results {
		stars := strings.Repeat("⭐", r.Importance)
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, r.Topic, r.Content)
		fmt.Fprintf(&b, "   重要性: %s  来源: %s  访问: %d次\n", stars, r.Source, r.AccessCount)
		fmt.Fprintf(&b, "   FTS 分: %.2f\n", r.Rank)
		fmt.Fprintln(&b)
	}

	return b.String()
}
