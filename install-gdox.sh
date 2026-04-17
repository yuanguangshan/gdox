#!/usr/bin/env bash
# 项目文档生成工具安装脚本（全局可用 + gd 快捷命令）
# 支持 --uninstall 卸载

set -e

# ============================================================
#  卸载模式
# ============================================================
if [[ "$1" == "--uninstall" ]]; then
    echo "🗑 卸载 gdox..."

    FOUND=false
    for DIR in "/usr/local/bin" "$HOME/.local/bin"; do
        if [[ -f "$DIR/gdox" ]]; then
            FOUND=true
            if [[ -w "$DIR" ]]; then
                rm -f "$DIR/gdox" "$DIR/gd"
                echo "✓ 已从 $DIR 移除 gdox 和 gd"
            elif command -v sudo &> /dev/null; then
                sudo rm -f "$DIR/gdox" "$DIR/gd"
                echo "✓ 已从 $DIR 移除 gdox 和 gd (sudo)"
            else
                echo "❌ 无权限删除 $DIR/gdox，请手动删除"
                exit 1
            fi
        fi
    done

    if [[ "$FOUND" == false ]]; then
        echo "⚠️  未找到已安装的 gdox"
    else
        echo "✅ 卸载完成"
    fi
    exit 0
fi

# ============================================================
#  安装模式
# ============================================================
echo "🚀 开始安装 gdox..."

# -------- 前置检查：源文件 --------
if [[ ! -f "gdox.go" ]]; then
    echo "❌ 未找到 gdox.go"
    echo "请在包含 gdox.go 的目录中运行此脚本"
    exit 1
fi

# -------- 前置检查：Go 编译器 --------
if ! command -v go &> /dev/null; then
    echo "❌ 未检测到 Go 编译器"
    echo "请先安装 Go: https://go.dev/dl/"
    exit 1
fi

echo "✓ Go 版本: $(go version)"

# -------- 编译 --------
echo "📦 编译 gdox..."
go build -o gdox gdox.go

# -------- 选择安装目录 --------
if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
    USE_SUDO=""
elif command -v sudo &> /dev/null; then
    INSTALL_DIR="/usr/local/bin"
    USE_SUDO="sudo"
else
    INSTALL_DIR="$HOME/.local/bin"
    USE_SUDO=""
    mkdir -p "$INSTALL_DIR"
fi

echo "📍 安装目录: $INSTALL_DIR"

# -------- 检查已有安装 --------
if [[ -f "$INSTALL_DIR/gdox" ]]; then
    EXISTING_VERSION=$("$INSTALL_DIR/gdox" -version 2>/dev/null || echo "unknown")
    echo "⚠️  检测到已安装版本: $EXISTING_VERSION，将覆盖安装..."
fi

# -------- 安装主程序 --------
echo "📥 安装 gdox"
$USE_SUDO mv gdox "$INSTALL_DIR/gdox"
$USE_SUDO chmod +x "$INSTALL_DIR/gdox"

# -------- 创建 gd 快捷命令（软链接） --------
echo "🔗 创建 gd 快捷命令"
$USE_SUDO ln -sf "$INSTALL_DIR/gdox" "$INSTALL_DIR/gd"

# -------- PATH 检查（仅在用户目录时） --------
if [[ "$INSTALL_DIR" == "$HOME/.local/bin" ]]; then
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        echo ""
        echo "⚠️  $INSTALL_DIR 不在 PATH 中"
        echo ""
        echo "请将以下内容加入你的 shell 配置文件："
        echo ""
        echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
        echo "然后执行:"
        echo "    source ~/.zshrc  或  source ~/.bashrc"
    else
        echo "✓ PATH 已正确配置"
    fi
fi

# -------- 验证安装 --------
NEW_VERSION=$("$INSTALL_DIR/gdox" -version 2>/dev/null || echo "")

# -------- 完成 --------
echo ""
echo "✅ 安装完成！${NEW_VERSION:+ ($NEW_VERSION)}"
echo ""
echo "现在你可以在任意目录使用："
echo "  gdox         # 完整命令"
echo "  gd           # 快捷命令"
echo ""
echo "示例："
echo "  gd                                 # 扫描当前目录"
echo "  gd -i md,go                        # 只包含特定后缀"
echo "  gd -x exe,bin                      # 排除特定后缀"
echo "  gd -m _test.go                     # 模糊匹配：提取所有测试文件"
echo "  gd -m _test.go -xm vendor/         # 复合匹配：包含测试 but 排除第三方库"
echo "  gd -ns                             # 不扫描子目录"
echo "  gd --dry-run                       # 预览模式，不实际生成文件"
echo "  gd --no-default-ignore             # 禁用默认忽略规则"
echo "  gd --no-gitignore                  # 不加载 .gitignore"
echo "  gd --ignore generated,proto        # 额外忽略特定目录/文件"
echo ""
echo "卸载："
echo "  bash install-gdox.sh --uninstall"
echo ""
