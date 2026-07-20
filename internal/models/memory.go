// Package models 定义记忆数据模型
package models

// Memory 单条记忆记录
type Memory struct {
	ID          int64   `json:"id"`
	Content     string  `json:"content"`
	Topic       string  `json:"topic"`
	Source      string  `json:"source"`
	Importance  int     `json:"importance"`
	CreatedAt   string  `json:"created_at"`
	AccessedAt  string  `json:"accessed_at"`
	AccessCount int     `json:"access_count"`
	Archived    bool    `json:"archived"`
	Rank        float64 `json:"rank,omitempty"`
}

// ByRank 实现排序接口，按 rank 降序
type ByRank []Memory

func (a ByRank) Len() int           { return len(a) }
func (a ByRank) Less(i, j int) bool { return a[i].Rank > a[j].Rank }
func (a ByRank) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

// ByImportance 实现排序接口，按重要性降序
type ByImportance []Memory

func (a ByImportance) Len() int           { return len(a) }
func (a ByImportance) Less(i, j int) bool { return a[i].Importance > a[j].Importance }
func (a ByImportance) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
