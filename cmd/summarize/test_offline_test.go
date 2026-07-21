package main

import (
	"strings"
	"testing"

	"github.com/MrTreasure/picoclaw-memory/internal/models"
)

func TestTrigrams(t *testing.T) {
	grams := trigrams("hello world")
	if len(grams) == 0 {
		t.Error("expected non-empty trigrams")
	}
}

func TestJaccardIdentical(t *testing.T) {
	sim := jaccardSim("hello world", "hello world")
	if sim != 1.0 {
		t.Errorf("expected 1.0, got %.4f", sim)
	}
}

func TestJaccardEmpty(t *testing.T) {
	sim := jaccardSim("", "")
	if sim != 0.0 {
		t.Errorf("expected 0.0, got %.4f", sim)
	}
}

func TestJaccardSimilar(t *testing.T) {
	a := "今天会议讨论了项目进度"
	b := "今天会议讨论了项目进度情况"
	sim := jaccardSim(a, b)
	if sim < 0.5 {
		t.Errorf("expected high similarity for similar Chinese text, got %.4f", sim)
	}
}

func TestJaccardDifferent(t *testing.T) {
	a := "今天的会议讨论了项目进度"
	b := "中午吃了麻辣香锅味道不错"
	sim := jaccardSim(a, b)
	if sim > 0.3 {
		t.Errorf("expected low similarity for different Chinese text, got %.4f", sim)
	}
}

func TestDedupNoDupes(t *testing.T) {
	entries := []models.Memory{
		{Content: "aaa bbb ccc", Importance: 3, AccessedAt: "2026-07-20T10:00:00Z"},
		{Content: "xxx yyy zzz", Importance: 2, AccessedAt: "2026-07-20T11:00:00Z"},
	}
	result := dedupEntries(entries, 0.6)
	if len(result) != 2 {
		t.Errorf("expected 2 entries (no dupes), got %d", len(result))
	}
}

func TestDedupWithDupes(t *testing.T) {
	entries := []models.Memory{
		{Content: "今天会议讨论了项目进度", Importance: 3, AccessedAt: "2026-07-20T10:00:00Z"},
		{Content: "今天会议讨论了项目进度情况", Importance: 4, AccessedAt: "2026-07-20T10:00:00Z"},
	}
	result := dedupEntries(entries, 0.6)
	if len(result) != 1 {
		t.Errorf("expected 1 entry after dedup, got %d", len(result))
	}
	// 应该保留更高 importance 的那条
	if result[0].Importance != 4 {
		t.Errorf("expected to keep entry with higher importance (4), got %d", result[0].Importance)
	}
}

func TestOfflineSummarizeBasic(t *testing.T) {
	entries := []models.Memory{
		{Topic: "work", Content: "项目 A 验收通过", Importance: 4, AccessedAt: "2026-07-20T10:00:00Z"},
		{Topic: "work", Content: "项目 A 验收成功了", Importance: 3, AccessedAt: "2026-07-20T11:00:00Z"},
		{Topic: "life", Content: "周末去爬山了", Importance: 2, AccessedAt: "2026-07-19T10:00:00Z"},
		{Topic: "life", Content: "周末爬山很开心", Importance: 2, AccessedAt: "2026-07-19T11:00:00Z"},
		{Topic: "work", Content: "修复了登录 bug", Importance: 3, AccessedAt: "2026-07-18T10:00:00Z"},
		{Topic: "work", Content: "修复了登录问题", Importance: 3, AccessedAt: "2026-07-18T11:00:00Z"},
		{Topic: "work", Content: "登录 bug 已修复", Importance: 2, AccessedAt: "2026-07-18T12:00:00Z"},
		{Topic: "life", Content: "看了部电影", Importance: 1, AccessedAt: "2026-07-17T10:00:00Z"},
	}
	result := offlineSummarize(entries, true, "summary:week-2026-W29")

	// 验证 topic
	for _, e := range result {
		if e.Topic != "summary:week-2026-W29" {
			t.Errorf("expected topic 'summary:week-2026-W29', got %q", e.Topic)
		}
	}
	// 验证重要性为 4（weekly）
	for _, e := range result {
		if e.Importance != 4 {
			t.Errorf("expected importance 4 for weekly, got %d", e.Importance)
		}
	}
	// 验证每个 topic 不超过 3 条
	workCount, lifeCount := 0, 0
	for _, e := range result {
		if strings.Contains(e.Content, "项目") || strings.Contains(e.Content, "修复") || strings.Contains(e.Content, "登录") || strings.Contains(e.Content, "bug") {
			workCount++
		} else {
			lifeCount++
		}
	}
	if workCount > 3 {
		t.Errorf("expected at most 3 work entries, got %d", workCount)
	}
	if lifeCount > 3 {
		t.Errorf("expected at most 3 life entries, got %d", lifeCount)
	}
	// 验证去重后总数少于原始
	if len(result) >= len(entries) {
		t.Errorf("expected fewer entries after dedup+trim, got %d >= %d", len(result), len(entries))
	}
}

func TestOfflineSummarizeMonthly(t *testing.T) {
	entries := []models.Memory{
		{Topic: "work", Content: "项目 A 验收通过", Importance: 4, AccessedAt: "2026-07-20T10:00:00Z"},
		{Topic: "life", Content: "周末去爬山了", Importance: 2, AccessedAt: "2026-07-19T10:00:00Z"},
	}
	result := offlineSummarize(entries, false, "summary:month-202607")
	for _, e := range result {
		if e.Importance != 6 {
			t.Errorf("expected importance 6 for monthly, got %d", e.Importance)
		}
	}
}

func TestCropToThreePerTopic(t *testing.T) {
	entries := []models.Memory{
		{Topic: "work", Content: "A任务完成", Importance: 4, AccessedAt: "2026-07-20T10:00:00Z"},
		{Topic: "work", Content: "B任务完成", Importance: 4, AccessedAt: "2026-07-19T10:00:00Z"},
		{Topic: "work", Content: "C任务完成", Importance: 3, AccessedAt: "2026-07-18T10:00:00Z"},
		{Topic: "work", Content: "D任务完成", Importance: 3, AccessedAt: "2026-07-17T10:00:00Z"},
		{Topic: "work", Content: "E任务完成", Importance: 2, AccessedAt: "2026-07-16T10:00:00Z"},
	}
	result := offlineSummarize(entries, true, "summary:week-2026-W29")
	// 所有条目都是 work topic，应该只有 3 条
	if len(result) > 3 {
		t.Errorf("expected at most 3 entries, got %d", len(result))
	}
}
