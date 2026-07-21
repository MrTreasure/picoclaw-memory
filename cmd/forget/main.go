// Command forget — 记忆遗忘与归档
// 遗忘规则:
//   - 重要性 ≤ 2 + 7天未访问 → archived=1 (归档)
//   - 重要性 ≤ 1 + 14天未访问 → 软删除 (deleted=1)
//   - 已归档且超过30天 → 软删除 (deleted=1)
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MrTreasure/picoclaw-memory/internal/config"
	"github.com/MrTreasure/picoclaw-memory/internal/db"
	"github.com/MrTreasure/picoclaw-memory/internal/models"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "模拟运行，不实际修改")
	purge := flag.Bool("purge", false, "物理删除所有已软删除的记录")
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

	// --purge 模式：物理删除
	if *purge {
		n, err := db.PurgeDeletedMemories(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 清理失败: %v\n", err)
			os.Exit(1)
		}
		stats, _ := db.GetStats(conn)
		fmt.Printf("📦 已物理删除 %d 条已软删除的记录\n\n", n)
		fmt.Printf("📊 当前状态:\n   总计: %d 条 | 活跃: %d 条 | 已归档: %d 条\n",
			stats["total"], stats["active"], stats["archived"])
		return
	}

	// 执行遗忘流程
	archived, deleted, skipped, err := db.ArchiveMemories(conn, *dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 遗忘流程失败: %v\n", err)
		os.Exit(1)
	}

	// 获取统计
	stats, err := db.GetStats(conn)
	if err != nil {
		stats = map[string]int{"total": 0, "active": 0, "archived": 0}
	}

	// 输出报告
	fmt.Print(formatReport(archived, deleted, skipped, stats, *dryRun))
}

func formatReport(archived, deleted int, skipped []models.Memory, stats map[string]int, dryRun bool) string {
	var b strings.Builder

	mode := "🔍 模拟运行"
	if !dryRun {
		mode = "📦 执行软删除清理"
	}
	fmt.Fprintf(&b, "=== %s ===\n", mode)
	fmt.Fprintf(&b, "时间: %s\n\n", time.Now().Format("2006-01-02 15:04"))

	if archived > 0 {
		fmt.Fprintf(&b, "📦 归档 (%d 条):\n", archived)
	}

	if deleted > 0 {
		fmt.Fprintf(&b, "📦 软删除 (%d 条):\n", deleted)
	}

	// 显示模拟运行中将被处理的条目
	if dryRun && len(skipped) > 0 {
		// 按类型分开显示
		var toArchive, toDelete []models.Memory
		for _, s := range skipped {
			// 重要性 ≤ 2 且未归档 → 将被归档
			if s.Importance <= 2 && !s.Archived {
				toArchive = append(toArchive, s)
			} else {
				toDelete = append(toDelete, s)
			}
		}

		if len(toArchive) > 0 {
			fmt.Fprintf(&b, "📦 将归档 (%d 条):\n", len(toArchive))
			displayLimit(toArchive, &b, 10)
			fmt.Fprintln(&b)
		}

		if len(toDelete) > 0 {
			fmt.Fprintf(&b, "📦 将软删除 (%d 条):\n", len(toDelete))
			displayLimit(toDelete, &b, 10)
			fmt.Fprintln(&b)
		}

		if len(skipped) == 0 {
			fmt.Fprintln(&b, "ℹ️  没有需要清理的记忆")
		}
	}

	if !dryRun && archived == 0 && deleted == 0 {
		fmt.Fprintln(&b, "ℹ️  没有需要清理的记忆")
	}

	// 统计数据
	fmt.Fprintf(&b, "\n📊 当前状态:\n")
	fmt.Fprintf(&b, "   总计: %d 条 | 活跃: %d 条 | 已归档: %d 条\n",
		stats["total"], stats["active"], stats["archived"])

	return b.String()
}

func displayLimit(items []models.Memory, b *strings.Builder, limit int) {
	for i, m := range items {
		if i >= limit {
			fmt.Fprintf(b, "   ... 还有 %d 条\n", len(items)-limit)
			break
		}
		content := m.Content
		if len(content) > 60 {
			content = content[:60]
		}
		fmt.Fprintf(b, "   [%s] %s\n", m.Topic, content)
	}
}
