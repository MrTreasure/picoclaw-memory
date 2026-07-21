// Command summarize — 记忆层级总结
// 周总结：聚合每日条目 → 生成周总结 → 归档原始条目
// 月总结：聚合周总结条目 → 生成月总结 → 归档周总结
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MrTreasure/picoclaw-memory/internal/config"
	"github.com/MrTreasure/picoclaw-memory/internal/db"
	"github.com/MrTreasure/picoclaw-memory/internal/models"
)

// trigram Jaccard 去重阈值
const dedupThreshold = 0.6

func main() {
	periodFlag := flag.String("period", "", "总结周期: weekly 或 monthly")
	dryRun := flag.Bool("dry-run", false, "模拟运行，不实际修改")
	modelFlag := flag.String("model", "", "使用的 LLM 模型（默认 config.llm_model）")
	dateFlag := flag.String("date", "", "参考日期（默认今天，用于测试指定某周/月）")
	offlineFlag := flag.Bool("offline", false, "离线模式：使用 trigram Jaccard 去重排序，不调用 LLM")
	flag.Parse()

	if *periodFlag == "" {
		fmt.Fprintf(os.Stderr, "❌ --period 必填 (weekly 或 monthly)\n")
		os.Exit(1)
	}
	if *periodFlag != "weekly" && *periodFlag != "monthly" {
		fmt.Fprintf(os.Stderr, "❌ --period 必须是 weekly 或 monthly\n")
		os.Exit(1)
	}

	// 加载配置
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	model := *modelFlag
	if model == "" {
		model = cfg.LLMModel
	}

	// 初始化数据库
	conn, err := db.InitDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 数据库初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// 计算时间范围和标签
	loc := time.FixedZone("CST", 8*3600)
	refDate := time.Now()
	if *dateFlag != "" {
		parsed, err := time.Parse("2006-01-02", *dateFlag)
		if err == nil {
			refDate = parsed
		}
	}

	var startDate, endDate time.Time
	var periodLabel, sourceType, targetSource, topicPrefix string
	var isWeekly bool

	if *periodFlag == "weekly" {
		isWeekly = true
		startDate, endDate = lastWeekRange(refDate)
		// ISO 周数需要用本地时间计算（startDate 已被转成 UTC）
		localStart := startDate.In(loc)
		year, week := localStart.ISOWeek()
		periodLabel = fmt.Sprintf("%d-W%02d", year, week)
		sourceType = "daily"
		targetSource = "weekly_summary"
		topicPrefix = "summary:week-"
	} else {
		isWeekly = false
		startDate, endDate = lastMonthRange(refDate)
		periodLabel = startDate.In(loc).Format("200601")
		sourceType = "weekly_summary"
		targetSource = "monthly_summary"
		topicPrefix = "summary:month-"
	}

	topicLabel := topicPrefix + periodLabel
	periodDisplay := fmt.Sprintf("%s ~ %s",
		startDate.In(loc).Format("01-02"),
		endDate.In(loc).Format("01-02"))

	// 检查是否已经总结过
	existing, err := db.CountBySourceAndTopic(conn, targetSource, topicLabel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 查询已有总结失败: %v\n", err)
		os.Exit(1)
	}
	if existing > 0 {
		fmt.Printf("ℹ️  %s 总结「%s」已存在 (%d 条)，跳过\n", *periodFlag, periodLabel, existing)
		return
	}

	// 查询源条目
	entries, err := db.GetMemoriesByDateRange(conn, startDate, endDate, sourceType, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 查询源条目失败: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Printf("ℹ️  %s (%s) 无新条目，跳过\n", *periodFlag, periodDisplay)
		return
	}

	fmt.Printf("📊 %s 总结「%s」(%s) — 共 %d 条源条目\n", *periodFlag, periodLabel, periodDisplay, len(entries))

	if *dryRun {
		printPreview(entries, targetSource, topicLabel)
		return
	}

	var summaryEntries []models.Memory

	if *offlineFlag {
		// ===== 离线模式：trigram Jaccard 去重排序 =====
		fmt.Println("   🔧 离线模式 — 使用 trigram Jaccard 去重排序")
		summaryEntries = offlineSummarize(entries, isWeekly, topicLabel)
		fmt.Printf("   ✅ 离线总结完成 — %d 条摘要（去重前 %d 条）\n", len(summaryEntries), len(entries))
	} else {
		// ===== 在线模式：调用 LLM =====
		prompt := buildPrompt(entries, isWeekly)

		fmt.Printf("   🤖 调用 %s 生成总结...\n", model)
		llmOutput, err := callLLM(cfg.PicoclawBin, model, prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ LLM 调用失败: %v\n", err)
			os.Exit(1)
		}

		summaryEntries = parseLLMResponse(llmOutput, topicLabel)
		if len(summaryEntries) == 0 {
			fmt.Fprintf(os.Stderr, "❌ 未能从 LLM 输出中解析出有效条目\n")
			fmt.Fprintf(os.Stderr, "   LLM 原始输出:\n%s\n", llmOutput)
			os.Exit(1)
		}

		fmt.Printf("   ✅ LLM 输出解析成功 — %d 条摘要\n", len(summaryEntries))
	}

	// 写入总结条目
	saved := saveSummaryEntries(summaryEntries, targetSource, conn)
	fmt.Printf("   💾 写入 %d 条 %s 总结\n", saved, *periodFlag)

	// 归档源条目
	sourceIDs := make([]int64, len(entries))
	for i, e := range entries {
		sourceIDs[i] = e.ID
	}
	archived, err := db.ArchiveByIDs(conn, sourceIDs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  归档源条目失败: %v\n", err)
	} else {
		fmt.Printf("   📦 归档 %d 条原始条目\n", archived)
	}

	// 统计
	stats, _ := db.GetStats(conn)
	fmt.Printf("\n📊 记忆库总计: %d 条 | 活跃: %d 条 | 已归档: %d 条\n",
		stats["total"], stats["active"], stats["archived"])
}

// ========== 离线模式 ==========

// offlineSummarize 离线总结：按 topic 分组 → 去重 → 排序 → 裁剪
func offlineSummarize(entries []models.Memory, isWeekly bool, topicLabel string) []models.Memory {
	// 1. 按 topic 分组
	groupMap := make(map[string][]models.Memory)
	var groupOrder []string
	for _, e := range entries {
		t := e.Topic
		if t == "" {
			t = "general"
		}
		if _, ok := groupMap[t]; !ok {
			groupOrder = append(groupOrder, t)
		}
		groupMap[t] = append(groupMap[t], e)
	}

	// 2. 设置摘要的 importance
	summaryImp := 4 // weekly_summary
	if !isWeekly {
		summaryImp = 6 // monthly_summary
	}

	var result []models.Memory

	for _, topic := range groupOrder {
		group := groupMap[topic]

		// 去重 — 按重要性降序后，用 trigram Jaccard 标记重复
		deduped := dedupEntries(group, dedupThreshold)

		// 排序 — 按 importance 降序，同等 imp 按 accessed_at 降序
		sort.SliceStable(deduped, func(i, j int) bool {
			if deduped[i].Importance != deduped[j].Importance {
				return deduped[i].Importance > deduped[j].Importance
			}
			return deduped[i].AccessedAt > deduped[j].AccessedAt
		})

		// 裁剪 — 每个 topic 保留前 3 条
		maxPerTopic := 3
		if len(deduped) > maxPerTopic {
			deduped = deduped[:maxPerTopic]
		}

		// 生成摘要条目
		for _, e := range deduped {
			result = append(result, models.Memory{
				Content:    e.Content,
				Topic:      topicLabel,
				Source:     "",     // 由 saveSummaryEntries 设置
				Importance: summaryImp,
			})
		}
	}

	return result
}

// dedupEntries 对同一 topic 下的条目去重
// 先按 importance 降序排序，再用 trigram Jaccard 比较
func dedupEntries(entries []models.Memory, threshold float64) []models.Memory {
	if len(entries) <= 1 {
		return entries
	}

	// 按 importance 降序（高 imp 优先保留）
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Importance != entries[j].Importance {
			return entries[i].Importance > entries[j].Importance
		}
		return entries[i].AccessedAt > entries[j].AccessedAt
	})

	keep := make([]bool, len(entries))
	for i := range keep {
		keep[i] = true
	}

	for i := 0; i < len(entries); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			if !keep[j] {
				continue
			}
			sim := jaccardSim(entries[i].Content, entries[j].Content)
			if sim > threshold {
				keep[j] = false
			}
		}
	}

	result := make([]models.Memory, 0, len(entries))
	for i, e := range entries {
		if keep[i] {
			result = append(result, e)
		}
	}
	return result
}

// trigrams 将字符串转换为 trigram 计数 map
func trigrams(s string) map[string]int {
	grams := make(map[string]int)
	runes := []rune(strings.ToLower(s))
	for i := 0; i+2 < len(runes); i++ {
		grams[string(runes[i:i+3])]++
	}
	return grams
}

// jaccardSim 计算两个字符串的 trigram Jaccard 相似度
func jaccardSim(a, b string) float64 {
	gramsA := trigrams(a)
	gramsB := trigrams(b)
	intersect, union := 0, 0
	for k := range gramsA {
		if gramsB[k] > 0 {
			intersect++
		}
		union++
	}
	for k := range gramsB {
		if gramsA[k] <= 0 {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

// ========== 在线模式（LLM） ==========

// lastWeekRange 返回上周一 ~ 上周日的 UTC 时间范围
func lastWeekRange(t time.Time) (start, end time.Time) {
	loc := time.FixedZone("CST", 8*3600)
	t = t.In(loc)
	y, m, d := t.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)

	// 距离上周一的偏移
	weekday := today.Weekday()
	daysSinceMonday := int(weekday - time.Monday)
	if daysSinceMonday < 0 {
		daysSinceMonday += 7
	}
	// 上周一
	lastMonday := today.AddDate(0, 0, -daysSinceMonday-7)
	// 上周日 23:59:59
	lastSunday := lastMonday.AddDate(0, 0, 6).Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	return lastMonday.UTC(), lastSunday.UTC()
}

// lastMonthRange 返回上个月 1 号 ~ 最后一天的 UTC 时间范围
func lastMonthRange(t time.Time) (start, end time.Time) {
	loc := time.FixedZone("CST", 8*3600)
	t = t.In(loc)

	thisMonth := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	lastMonthEnd := thisMonth.Add(-time.Second)
	lastMonthStart := time.Date(lastMonthEnd.Year(), lastMonthEnd.Month(), 1, 0, 0, 0, 0, loc)

	return lastMonthStart.UTC(), lastMonthEnd.UTC()
}

// buildPrompt 构建 LLM 提示词
func buildPrompt(entries []models.Memory, isWeekly bool) string {
	var b strings.Builder

	periodName := "周"
	periodEn := "weekly"
	if !isWeekly {
		periodName = "月"
		periodEn = "monthly"
	}

	fmt.Fprintf(&b, `你是一个 picoclaw 记忆系统的%s总结助手。请根据以下条目，归纳成 3-5 条核心记忆摘要。

要求：
1. 保持事实性，去掉冗余重复
2. 按重要性从高到低排序
3. 必须使用以下格式输出（重要），每条摘要标注 [imp:N]

## [主题名]
- 摘要内容 (imp:3)
- 摘要内容 (imp:4)

---

以下是待总结的%s记忆条目（按 topic 分组）：

`, periodName, periodEn)

	// 按 topic 分组展示
	type groupEntry struct {
		Topic   string
		Entries []models.Memory
	}
	groupMap := make(map[string][]models.Memory)
	var groupOrder []string
	for _, e := range entries {
		t := e.Topic
		if t == "" {
			t = "general"
		}
		if _, ok := groupMap[t]; !ok {
			groupOrder = append(groupOrder, t)
		}
		groupMap[t] = append(groupMap[t], e)
	}

	for _, t := range groupOrder {
		fmt.Fprintf(&b, "## [%s]\n", t)
		for _, e := range groupMap[t] {
			content := strings.TrimSpace(e.Content)
			if content == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s (imp:%d)\n", content, e.Importance)
		}
		fmt.Fprintln(&b)
	}

	b.WriteString("\n仅输出上述格式的内容，不要任何多余的文字。")

	return b.String()
}

// callLLM 调用 picoclaw agent
func callLLM(binPath, model, prompt string) (string, error) {
	cmd := exec.Command(binPath, "agent", "--model", model, "--no-color", "-m", prompt)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("exec picoclaw agent: %w\n%s", err, string(output))
	}
	return string(output), nil
}

// parseLLMResponse 解析 LLM 输出为记忆条目
func parseLLMResponse(llmOutput, topicLabel string) []models.Memory {
	var entries []models.Memory

	// 清理输出：去除 ASCII 彩色转义等
	clean := stripANSICodes(llmOutput)

	// 找到第一个 "## [" 开始解析
	lines := strings.Split(clean, "\n")
	topicRe := regexp.MustCompile(`^##\s*\[(.+?)\]`)
	entryRe := regexp.MustCompile(`^\s*[-*]\s+(.+?)\s*\(imp:(\d)\)\s*$`)
	altEntryRe := regexp.MustCompile(`^\s*[-*]\s+(.+?)$`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 检测 topic 标题 — 跳过，topic 统一用传入的标签
		if topicRe.MatchString(line) {
			continue
		}

		// 检测条目（优先带重要性标记）
		if match := entryRe.FindStringSubmatch(line); match != nil {
			content := strings.TrimSpace(match[1])
			imp, _ := parseInt(match[2])
			if imp < 1 {
				imp = 3
			}
			if imp > 5 {
				imp = 5
			}
			entries = append(entries, models.Memory{
				Content:    content,
				Topic:      topicLabel,
				Source:     "", // 调用方设置
				Importance: imp,
			})
			continue
		}

		// 无重要性标记的条目，默认 imp=3
		if match := altEntryRe.FindStringSubmatch(line); match != nil {
			content := strings.TrimSpace(match[1])
			// 跳过非条目行（标题、说明等）
			if strings.HasPrefix(content, "以下是") || strings.HasPrefix(content, "仅输出") ||
				strings.HasPrefix(content, "要求") || strings.HasPrefix(content, "你是一个") ||
				strings.Contains(content, "格式") {
				continue
			}
			entries = append(entries, models.Memory{
				Content:    content,
				Topic:      topicLabel,
				Source:     "",
				Importance: 3,
			})
		}
	}

	// 如果 topic 未设置，使用传入的标签
	for i := range entries {
		if entries[i].Topic == "" {
			entries[i].Topic = topicLabel
		}
	}

	return entries
}

// stripANSICodes 去除 ANSI 彩色转义序列
func stripANSICodes(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return re.ReplaceAllString(s, "")
}

// parseInt 安全解析整数
func parseInt(s string) (int, bool) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err == nil
}

// saveSummaryEntries 写入总结条目
func saveSummaryEntries(entries []models.Memory, source string, conn *sql.DB) int {
	count := 0
	for _, e := range entries {
		m := &models.Memory{
			Content:    e.Content,
			Topic:      e.Topic,
			Source:     source,
			Importance: e.Importance,
		}
		// 去重
		exists, err := db.ContentExists(conn, e.Content)
		if err != nil || exists {
			continue
		}
		_, err = db.InsertMemory(conn, m)
		if err == nil {
			count++
		}
	}
	return count
}

// printPreview 打印模拟结果
func printPreview(entries []models.Memory, targetSource, topicLabel string) {
	fmt.Printf("   🔍 [dry-run] 将生成 %s 类型总结，topic=%s\n", targetSource, topicLabel)
	fmt.Printf("   🔍 [dry-run] 将归档 %d 条源条目:\n", len(entries))
	showCount := 10
	if len(entries) < showCount {
		showCount = len(entries)
	}
	for i := 0; i < showCount; i++ {
		content := entries[i].Content
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		fmt.Printf("      [%s] %s\n", entries[i].Topic, content)
	}
	if len(entries) > showCount {
		fmt.Printf("      ... 还有 %d 条\n", len(entries)-showCount)
	}
	fmt.Println("   ℹ️  未实际修改数据库")
}
