#!/bin/bash
# setup.sh — Picoclaw Memory System 安装脚本
set -e

echo "=== Picoclaw Memory System Setup ==="

# 检测 Python 3
PYTHON=$(command -v python3 || command -v python)
if [ -z "$PYTHON" ]; then
    echo "❌ 未找到 Python 3"
    exit 1
fi
echo "✅ Python: $($PYTHON --version)"

# 默认安装路径
INSTALL_DIR="${1:-$HOME/picoclaw-memory}"
mkdir -p "$INSTALL_DIR"

# 复制文件
cp -r src/* "$INSTALL_DIR/"
cp config.json "$INSTALL_DIR/" 2>/dev/null || true

echo "✅ 文件已复制到 $INSTALL_DIR"

# 初始化数据库
cd "$INSTALL_DIR"
$PYTHON db.py

# 别名设置
SHELL_RC="$HOME/.bashrc"
if command -v zsh &>/dev/null; then
    SHELL_RC="$HOME/.zshrc"
fi

if ! grep -q "picoclaw-memory" "$SHELL_RC" 2>/dev/null; then
    cat >> "$SHELL_RC" << EOF

# Picoclaw Memory System
alias capture="$PYTHON $INSTALL_DIR/capture.py"
alias recall="$PYTHON $INSTALL_DIR/recall.py"
alias forget="$PYTHON $INSTALL_DIR/forget.py"
EOF
    echo "✅ 别名已添加到 $SHELL_RC"
    echo "   运行 source $SHELL_RC 或重新打开终端使别名生效"
fi

echo ""
echo "=== 安装完成 ==="
echo "使用方式:"
echo "  python3 capture.py              # 采集今日记忆"
echo "  python3 recall.py '关键词'       # 搜索记忆"
echo "  python3 recall.py               # 显示最近记忆"
echo "  python3 forget.py --dry-run     # 模拟清理"
echo "  python3 forget.py               # 执行清理"
