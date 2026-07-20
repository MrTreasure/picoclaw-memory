# Picoclaw Memory System

轻量、零依赖、单二进制的 AI Agent 记忆系统。基于 Go + SQLite FTS5。

## 为什么不是 Mem0 / PowerMem

| 特性 | Mem0 | PowerMem | **picoclaw-memory** |
|------|------|----------|-------------------|
| 依赖 | 20+ 包 | 30+ 包 | **零依赖（Go 单二进制）** |
| 安装体积 | ~500MB | ~800MB | **~7MB** |
| Embedding 模型 | 必须 | 必须 | **不需要** |
| 存储 | Chroma/Redis | Chroma/Postgres | **SQLite (WAL + FTS5)** |
| 搜索 | Vector + hybrid | Vector + hybrid | **FTS5 全文搜索 + LIKE 兜底** |
| 冷启动 | 5-10 min | 10-15 min | **instant** |

## 特性

- **零外部依赖** — 纯 Go 编译，单个二进制文件，无运行时依赖
- **FTS5 全文搜索** — 无需 embedding 模型的快速检索
- **每日采集** — 从 Daily Note 自动提取事实
- **中文友好** — FTS5 兜底 LIKE 搜索，处理中文单字符分词问题
- **重要性标记** — 支持 `[imp:1-5]` 自定义重要性
- **自动遗忘** — 按策略归档或删除旧记忆
- **SQLite 存储** — 单文件，易备份和迁移

## 快速开始

```bash
# 安装二进制到 PATH
cp capture /usr/local/bin/pcm-capture
cp recall /usr/local/bin/pcm-recall
cp forget /usr/local/bin/pcm-forget
cp config.json /usr/local/bin/

# 采集今天的记忆
pcm-capture

# 搜索记忆
pcm-recall "关键词"

# 清理旧记忆
pcm-forget --dry-run
```

## 使用方法

### 采集每日记忆

```bash
# 自动检测今天
pcm-capture

# 指定日期
pcm-capture --date 2026-07-20
```

从 `memory/YYYYMM/YYYYMMDD.md` 提取：
- 列表项（`- 内容`）→ 重要性 3
- 任务（`- [x] 内容`）→ 标记为 tasks
- 含 `[imp:N]` 标记 → 自定义重要性

### 搜索记忆

```bash
# 全文搜索
pcm-recall "关键词"

# 按 topic 过滤
pcm-recall "AI" --topic notes

# 限制条数
pcm-recall "LLM" --top 5

# JSON 输出
pcm-recall "agent" --json
```

### 记忆维护

```bash
# 模拟运行
pcm-forget --dry-run

# 执行清理
pcm-forget
```

遗忘规则：
- 重要性 ≤ 2 + 7天未访问 → 归档
- 重要性 ≤ 1 + 14天未访问 → 删除
- 已归档且超过 30 天 → 删除

## 配置

编辑 `config.json`：

```json
{
  "memory_dir": "/path/to/notes",
  "db_path": "/path/to/memory.db",
  "retention_days": {
    "daily": 7,
    "weekly": 30,
    "monthly": 365
  }
}
```

## 项目结构

```
picoclaw-memory/
├── cmd/
│   ├── capture/main.go    # 每日记忆采集
│   ├── recall/main.go     # 记忆检索
│   └── forget/main.go     # 记忆遗忘
├── internal/
│   ├── config/config.go   # 配置加载
│   ├── db/db.go           # SQLite CRUD + FTS5 + 遗忘逻辑
│   └── models/memory.go   # 数据模型
├── config.json            # 配置文件
├── go.mod / go.sum
├── README.md
└── LICENSE
```

## 与 AI Agent 集成

通过 CLI 调用：

```bash
# 获取最近记忆
pcm-recall --top 5

# 搜索相关记忆
pcm-recall "用户偏好"

# 采集当前对话
pcm-capture
```

## 许可证

MIT
