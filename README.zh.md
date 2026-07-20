# Picoclaw Memory System

一个轻量级、零依赖的 AI Agent 记忆系统。替代 PowerMem / Mem0，只用 Python 3.11 内置库——没有 numpy、没有 sentence-transformers、没有 pip install。

## 为什么不用 Mem0 / PowerMem？

| 对比项 | Mem0 | PowerMem | **Picoclaw Memory** |
|--------|------|----------|-------------------|
| 依赖数量 | 20+ 个包 | 30+ 个包 | **零依赖** |
| 安装体积 | ~500MB | ~800MB | **~20KB** |
| Embedding 模型 | 必须 | 必须 | **不需要** |
| 存储引擎 | Chroma/Redis | Chroma/Postgres | **SQLite** |
| 搜索方式 | Vector + Hybrid | Vector + Hybrid | **FTS5 全文搜索** |
| 冷启动时间 | 5-10 分钟 | 10-15 分钟 | **瞬间** |

**如果你在构建百万级文档的生产 RAG 管线**，用 Mem0。  
**如果你只是想让 AI Agent 记住每天对话里的东西**，用这个。

## 特性

- **零外部依赖** — 只用 Python 3.11+ 内置模块（`sqlite3`, `json`, `re`, `datetime`, `pathlib`）
- **每日自动采集** — 从 daily note 自动提取事实
- **FTS5 全文搜索** — 不需要 embedding 模型，快速检索
- **重要性评分** — 用 `[imp:1-5]` 标记控制记忆权重
- **自动遗忘** — 按规则自动归档或删除过期记忆
- **SQLite 存储** — 单文件，易备份，易迁移

## 快速开始

```bash
# 克隆
git clone https://github.com/MrTreasure/picoclaw-memory.git
cd picoclaw-memory

# 初始化（不需要 pip install！）
python3 src/db.py

# 采集今日记忆
python3 src/capture.py --date 2026-07-20

# 搜索
python3 src/recall.py "AI Agent"
python3 src/recall.py "" --top 20    # 查看最近记忆

# 清理过期记忆
python3 src/forget.py --dry-run       # 预览
python3 src/forget.py                 # 执行
```

或者用安装脚本：

```bash
bash setup.sh
```

## 使用说明

### 采集每日记忆

```bash
# 自动检测今天
python3 src/capture.py

# 指定日期
python3 src/capture.py --date 2026-07-20
```

Capture 从 `memory/YYYYMM/YYYYMMDD.md` 读取：
- 列表项（`- 内容`）→ 默认重要性 3
- 任务（`- [x] 内容`）→ 标记为 tasks
- 带 `[imp:N]` 标记 → 自定义重要性

### 搜索记忆

```bash
# 全文搜索
python3 src/recall.py "关键词"

# 按 topic 过滤
python3 src/recall.py "AI" --topic notes

# 限制数量
python3 src/recall.py "Agent" --top 5

# JSON 输出
python3 src/recall.py "记忆" --json
```

### 记忆维护

```bash
# 预览清理结果
python3 src/forget.py --dry-run

# 执行
python3 src/forget.py
```

遗忘规则：
- 重要性 ≤ 2 + 7 天未访问 → 归档
- 重要性 ≤ 1 + 14 天未访问 → 删除

## 集成到 AI Agent

在 Agent 工作流中调用：

```python
import subprocess

# 每日采集
subprocess.run(["python3", "capture.py"])

# 查询
result = subprocess.run(
    ["python3", "recall.py", "用户偏好", "--json"],
    capture_output=True, text=True
)
```

## 项目结构

```
picoclaw-memory/
├── src/
│   ├── db.py         # 数据库初始化
│   ├── capture.py    # 每日记忆采集
│   ├── recall.py     # 记忆搜索
│   └── forget.py     # 记忆遗忘与归档
├── config.json       # 配置模板
├── setup.sh          # 安装脚本
├── LICENSE           # MIT
├── README.zh.md      # 中文文档
├── README.md         # 英文文档
└── .gitignore
```

## 配置

编辑 `config.json`：

```json
{
  "memory_dir": "/path/to/memory",
  "db_path": "/path/to/memory.db"
}
```

## License

MIT
