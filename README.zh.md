# Picoclaw Memory System

一个轻量级、零依赖、单二进制的 AI Agent 记忆系统。基于 **Go + SQLite FTS5**。

替代 PowerMem / Mem0——没有 Python、没有 embedding 模型、没有几百 MB 的 venv。

## 为什么不是 Mem0 / PowerMem？

| 对比项 | Mem0 | PowerMem | **Picoclaw Memory** |
|--------|------|----------|-------------------|
| 运行环境 | Python 3.8+ | Python 3.11+ | **Go 单二进制** |
| 依赖数量 | 20+ 个包 | 30+ 个包 | **零依赖** |
| 安装体积 | ~500MB | ~800MB | **~8MB** |
| Embedding 模型 | 必须 | 必须 | **不需要** |
| 存储引擎 | Chroma/Redis | Chroma/Postgres | **SQLite (WAL + FTS5)** |
| 搜索方式 | Vector + Hybrid | Vector + Hybrid | **FTS5 全文搜索 + LIKE 兜底** |
| 冷启动时间 | 5-10 分钟 | 10-15 分钟 | **瞬间** |
| 中文分词 | ❌ | ❌ | **FTS5 + LIKE 双引擎** |

**如果你在构建百万级文档的生产 RAG 管线** → 用 Mem0。  
**如果你只是想让 AI Agent 记住每天对话里的东西** → 用这个。

## 特性

- **零外部依赖** — 纯 Go 编译，单个二进制的文件，无运行时依赖
- **FTS5 全文搜索** — 无需 embedding 模型，快速检索
- **中文友好** — FTS5 兜底 LIKE 搜索，处理中文单字符分词问题
- **每日自动采集** — 从 Daily Note 自动提取事实、任务、备忘
- **重要性标记** — 支持 `[imp:1-5]` 自定义重要性权重
- **定期总结** — 自动将原始条目汇总为周/月摘要，压缩上下文
- **自动遗忘** — 按策略归档或删除过期记忆
- **SQLite 存储** — 单文件，易备份和迁移

## 架构

```
┌──────────────────────────────────────────┐
│               Pipeline                    │
│                                          │
│  📝 capture  ──→  📊 summarize  ──→  🗑️ forget │
│   (每日采集)       (周/月总结)       (自动遗忘)    │
│       │                │                  │
│       ▼                ▼                  │
│   SQLite DB  ←──────  FTS5  ←──────  recall    │
│   (原始)          (摘要)           (搜索)      │
└──────────────────────────────────────────┘
```

## 快速开始

```bash
# 编译所有二进制
git clone https://github.com/MrTreasure/picoclaw-memory.git
cd picoclaw-memory

# 需要 CGO（SQLite FTS5 依赖 C 库）
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-capture ./cmd/capture/
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-recall ./cmd/recall/
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-summarize ./cmd/summarize/
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-forget ./cmd/forget/

# 拷贝到 PATH
cp pcm-capture pcm-recall pcm-summarize pcm-forget config.json /usr/local/bin/

# 采集今天的记忆
pcm-capture

# 搜索
pcm-recall "关键词"
```

## 使用说明

### 📝 capture — 每日记忆采集

```bash
# 自动检测今天
pcm-capture

# 指定日期
pcm-capture --date 2026-07-20
```

从 `memory/YYYYMM/YYYYMMDD.md` 提取：
- 列表项（`- 内容`）→ 默认重要性 3
- 任务（`- [x] 内容` 或 `## 今日任务` 下）→ 标记为 tasks
- 带 `[imp:N]` 标记 → 自定义重要性（1-5）
- 支持去重：相同日期相同内容不会重复入库

### 📊 summarize — 定期总结

```bash
# 周总结
pcm-summarize --period weekly

# 月总结
pcm-summarize --period monthly
```

自动做这些事：
1. 读取未总结的原始 daily 条目
2. 调用 LLM（通过 `picoclaw agent` CLI）生成浓缩摘要
3. 写入 summary 条目到 DB（重要性继承）
4. 原始条目标记为已归档

### 🔍 recall — 记忆搜索

```bash
# 全文搜索
pcm-recall "关键词"

# 按 topic 过滤
pcm-recall "AI" --topic notes

# 限制条数
pcm-recall "LLM" --top 5

# JSON 输出
pcm-recall "agent" --json

# 包含已归档条目
pcm-recall "WAIC" --archived

# 按重要性过滤
pcm-recall "记忆" --importance 4
```

### 🗑️ forget — 自动遗忘

```bash
# 预览清理结果
pcm-forget --dry-run

# 执行清理
pcm-forget
```

遗忘规则：
- 重要性 ≤ 2 + 7 天未访问 → 归档（`archived=1`）
- 重要性 ≤ 1 + 14 天未访问 → **直接删除**
- 已归档且超过 30 天未访问 → **删除**
- 重要性 ≥ 3 的记忆：除非长期不访问，否则不会被清理

## 集成到 AI Agent

### 工作模式

Picoclaw Memory 是 AI Agent 的唯一记忆层，配合 AGENT.md 系统上下文使用：

```
┌──────────────────────────────┐
│      AGENT.md                │  ← session 启动时固定加载（极简关键信息）
│  家庭/设备/安全/端口          │
└──────────────────────────────┘
         │
         ▼
┌──────────────────────────────┐
│   SQLite FTS5 (memory.db)    │  ← 按需搜索（pcm-recall）
│  每日采集 + 自动遗忘          │
└──────────────────────────────┘
```

### 集成方式

作为 Skill 集成：

```yaml
# SKILL.md 中定义
pcm-recall "用户偏好"
```

或者通过 Agent CLI 直接调用：

```bash
# 获取相关记忆作为上下文
pcm-recall --top 5

# 搜索用户偏好
pcm-recall "偏好" --importance 4

# 采集当前对话
pcm-capture --date $(date +%F)
```

### 与 AI 模型配合

`pcm-summarize` 支持通过 `picoclaw agent` 调用模型进行总结：

```bash
# 使用指定模型总结
picoclaw agent --model qwen3.7-plus -m "总结以下内容: ..."
```

## 配置

编辑 `config.json`：

```json
{
  "workspace": "/path/to/workspace",
  "memory_dir": "/path/to/workspace/memory",
  "db_path": "/path/to/workspace/memory/memory.db",
  "picoclaw_bin": "/path/to/picoclaw",
  "llm_model": "qwen3.7-plus",
  "retention_days": {
    "daily": 7,
    "weekly": 30,
    "monthly": 365
  }
}
```

| 字段 | 说明 | 默认 |
|------|------|------|
| workspace | 工作区根目录 | `/path/to/workspace` |
| memory_dir | Daily Note 存放目录 | `{workspace}/memory` |
| db_path | SQLite 数据库路径 | `{memory_dir}/memory.db` |
| picoclaw_bin | picoclaw CLI 路径（用于 LLM 调用） | - |
| llm_model | 总结使用的模型 | `qwen3.7-plus` |
| retention_days.daily | daily 条目保留天数 | 7 |
| retention_days.weekly | weekly 摘要保留天数 | 30 |
| retention_days.monthly | monthly 摘要保留天数 | 365 |

## 项目结构

```
picoclaw-memory/
├── cmd/
│   ├── capture/main.go     # 每日记忆采集
│   ├── recall/main.go      # 记忆检索
│   ├── summarize/main.go   # 定期总结（周/月）
│   └── forget/main.go      # 记忆遗忘与归档
├── internal/
│   ├── config/config.go    # 配置加载
│   ├── db/db.go            # SQLite CRUD + FTS5 + 遗忘逻辑
│   └── models/memory.go    # 数据模型
├── config.json             # 配置文件
├── config.json.example     # 配置模板
├── go.mod / go.sum
├── README.zh.md            # 中文文档（本文件）
├── README.md               # 英文文档
├── setup.sh                # 安装脚本
└── LICENSE                 # MIT
```

## 技术细节

### 为何用 SQLite FTS5 而不是 embedding？

| 因素 | FTS5 | Embedding |
|------|------|-----------|
| 依赖 | ✅ CGO（编译时） | ❌ numpy + ONNX/Transformers |
| 部署 | ✅ 单文件 | ❌ 模型下载 500MB+ |
| 精确匹配 | ✅ 支持 | ❌ 模糊 |
| 语义搜索 | ⚠️ 有限 | ✅ 强 |
| 冷启动 | ✅ 即刻 | ❌ 5 分钟 |
| 中文分词 | ⚠️ LIKE 兜底 | ✅ 好 |

**结论**：对于 Agent 对话记忆规模（数百到数千条），FTS5 足够好。如果未来需要语义搜索，可以加 embedding 作为辅助引擎而非替代。

### 编译要求

- Go 1.23+
- CGO_ENABLED=1（SQLite FTS5 需要 C 库）
- 编译时必须带 `-tags sqlite_fts5`

```bash
# 正确编译方式
CGO_ENABLED=1 go build -tags sqlite_fts5 -o pcm-capture ./cmd/capture/
```

## License

MIT
