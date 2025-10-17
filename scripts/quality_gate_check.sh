#!/bin/bash
# Shannon 质量门禁检查脚本
# 遵循首席工程师行动手册框架

set -e

echo "🔍 Shannon 质量门禁检查"
echo "========================================="

FAILED=0

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 打印结果函数
print_result() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✅ $2${NC}"
    else
        echo -e "${RED}❌ $2${NC}"
        FAILED=1
    fi
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# 1. 检查 Python 代码质量
echo ""
echo "📝 检查 1: Python 代码质量"
echo "-----------------------------------------"

if command -v python3 &> /dev/null; then
    # 检查 Python 语法
    python3 -m py_compile benchmarks/*.py 2>/dev/null
    print_result $? "Python 语法检查"
    
    # 检查是否有单元测试
    if [ -d "benchmarks/tests" ] && [ -f "benchmarks/tests/test_workflow_bench.py" ]; then
        print_result 0 "单元测试存在"
    else
        print_result 1 "缺少单元测试"
    fi
else
    print_warning "Python3 未安装，跳过 Python 检查"
fi

# 2. 检查文档完整性
echo ""
echo "📚 检查 2: 文档完整性"
echo "-----------------------------------------"

REQUIRED_DOCS=(
    "PR_DESCRIPTION.md"
    "ENGINEERING_ANALYSIS.md"
    "benchmarks/README.md"
    "CONTRIBUTIONS.md"
)

for doc in "${REQUIRED_DOCS[@]}"; do
    if [ -f "$doc" ]; then
        print_result 0 "$doc 存在"
    else
        print_result 1 "$doc 缺失"
    fi
done

# 检查中文文档
if [ -d "docs/zh-CN" ]; then
    ZH_DOCS=$(find docs/zh-CN -name "*.md" | wc -l)
    if [ $ZH_DOCS -gt 0 ]; then
        print_result 0 "中文文档存在 ($ZH_DOCS 个文件)"
    else
        print_result 1 "中文文档目录为空"
    fi
else
    print_warning "中文文档目录不存在"
fi

# 3. 检查 Dockerfile 存在性
echo ""
echo "🐳 检查 3: Docker 配置"
echo "-----------------------------------------"

DOCKERFILES=(
    "go/orchestrator/Dockerfile"
    "rust/agent-core/Dockerfile"
    "python/llm-service/Dockerfile"
)

for dockerfile in "${DOCKERFILES[@]}"; do
    if [ -f "$dockerfile" ]; then
        print_result 0 "$dockerfile 存在"
    else
        print_result 1 "$dockerfile 缺失"
    fi
done

# 检查 Docker Compose 配置
if [ -f "deploy/compose/docker-compose.yml" ]; then
    print_result 0 "docker-compose.yml 存在"
else
    print_result 1 "docker-compose.yml 缺失"
fi

# 4. 检查 CI/CD 配置
echo ""
echo "⚙️  检查 4: CI/CD 配置"
echo "-----------------------------------------"

CI_WORKFLOWS=(
    ".github/workflows/benchmark.yml"
    ".github/workflows/docker-build.yml"
)

for workflow in "${CI_WORKFLOWS[@]}"; do
    if [ -f "$workflow" ]; then
        print_result 0 "$(basename $workflow) 存在"
    else
        print_result 1 "$(basename $workflow) 缺失"
    fi
done

# 5. 检查 Makefile 命令
echo ""
echo "🔧 检查 5: Makefile 命令"
echo "-----------------------------------------"

if [ -f "Makefile" ]; then
    print_result 0 "Makefile 存在"
    
    # 检查关键命令
    REQUIRED_TARGETS=("bench" "test" "docker-build")
    for target in "${REQUIRED_TARGETS[@]}"; do
        if grep -q "^$target:" Makefile; then
            print_result 0 "Makefile target '$target' 存在"
        else
            print_warning "Makefile target '$target' 不存在（可能是可选的）"
        fi
    done
else
    print_result 1 "Makefile 缺失"
fi

# 6. 检查文件编码
echo ""
echo "🔤 检查 6: 文件编码"
echo "-----------------------------------------"

if command -v file &> /dev/null; then
    # 检查中文 Markdown 文件是否为 UTF-8
    UTF8_CHECK=0
    for mdfile in docs/zh-CN/*.md 2>/dev/null; do
        if [ -f "$mdfile" ]; then
            if file -i "$mdfile" | grep -q "utf-8"; then
                UTF8_CHECK=$((UTF8_CHECK+1))
            else
                echo "⚠️  $(basename $mdfile) 可能不是 UTF-8 编码"
                FAILED=1
            fi
        fi
    done
    if [ $UTF8_CHECK -gt 0 ]; then
        print_result 0 "中文文档 UTF-8 编码检查 ($UTF8_CHECK 个文件)"
    fi
else
    print_warning "file 命令未安装，跳过编码检查"
fi

# 7. 检查技术债务标记
echo ""
echo "💳 检查 7: 技术债务管理"
echo "-----------------------------------------"

if [ -f "ENGINEERING_ANALYSIS.md" ]; then
    TODO_COUNT=$(grep -c "TODO" ENGINEERING_ANALYSIS.md 2>/dev/null || echo 0)
    TD_COUNT=$(grep -c "TD-" ENGINEERING_ANALYSIS.md 2>/dev/null || echo 0)
    
    print_result 0 "技术债务已记录 (TODO: $TODO_COUNT, TD: $TD_COUNT)"
else
    print_warning "ENGINEERING_ANALYSIS.md 不存在，无法检查技术债务"
fi

# 8. 检查断链（删除文件的引用）
echo ""
echo "🔗 检查 8: 文档链接完整性"
echo "-----------------------------------------"

# 检查是否有引用已删除文件的链接
DELETED_FILES=()
if git diff --name-status main 2>/dev/null | grep "^D" > /dev/null; then
    while IFS= read -r line; do
        file=$(echo "$line" | awk '{print $2}')
        DELETED_FILES+=("$file")
    done < <(git diff --name-status main 2>/dev/null | grep "^D")
    
    if [ ${#DELETED_FILES[@]} -gt 0 ]; then
        print_warning "检测到 ${#DELETED_FILES[@]} 个已删除文件，检查断链..."
        
        BROKEN_LINKS=0
        for deleted in "${DELETED_FILES[@]}"; do
            filename=$(basename "$deleted")
            # 在所有 Markdown 文件中搜索引用
            if grep -r "$filename" docs/ *.md 2>/dev/null | grep -v "ENGINEERING_ANALYSIS.md" > /dev/null; then
                echo "  ⚠️  发现对已删除文件的引用: $filename"
                BROKEN_LINKS=$((BROKEN_LINKS+1))
            fi
        done
        
        if [ $BROKEN_LINKS -eq 0 ]; then
            print_result 0 "未发现断链"
        else
            print_result 1 "发现 $BROKEN_LINKS 个可能的断链"
        fi
    else
        print_result 0 "未删除任何文件"
    fi
else
    print_warning "无法检查断链（不在 git 仓库或没有 main 分支）"
fi

# 9. 检查示例代码
echo ""
echo "📋 检查 9: 示例代码"
echo "-----------------------------------------"

if [ -d "examples" ]; then
    EXAMPLE_FILES=$(find examples -name "*.py" | wc -l)
    if [ $EXAMPLE_FILES -gt 0 ]; then
        print_result 0 "示例代码存在 ($EXAMPLE_FILES 个文件)"
        
        # 检查示例代码语法
        for example in examples/*.py; do
            if [ -f "$example" ]; then
                python3 -m py_compile "$example" 2>/dev/null
                if [ $? -eq 0 ]; then
                    print_result 0 "$(basename $example) 语法正确"
                else
                    print_result 1 "$(basename $example) 语法错误"
                fi
            fi
        done
    else
        print_warning "examples 目录为空"
    fi
else
    print_warning "examples 目录不存在"
fi

# 10. 生成报告
echo ""
echo "========================================="
echo "📊 质量门禁检查总结"
echo "========================================="

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✅ 所有检查通过！代码已准备好合并。${NC}"
    exit 0
else
    echo -e "${RED}❌ 部分检查失败。请修复后再提交。${NC}"
    echo ""
    echo "💡 提示："
    echo "  - 查看 ENGINEERING_ANALYSIS.md 了解详细的质量标准"
    echo "  - 运行 'make test' 执行完整测试套件"
    echo "  - 运行 'python -m pytest benchmarks/tests/' 执行单元测试"
    exit 1
fi

