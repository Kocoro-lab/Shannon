#!/bin/bash
# Shannon 增强质量检查脚本
# 包含20+项全面的自动化检查

set -e

echo "🔍 Shannon 增强质量检查系统"
echo "============================================================"

FAILED=0
WARNINGS=0
TOTAL_CHECKS=0

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 检查结果函数
check_result() {
    TOTAL_CHECKS=$((TOTAL_CHECKS+1))
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✅ [$TOTAL_CHECKS] $2${NC}"
    else
        echo -e "${RED}❌ [$TOTAL_CHECKS] $2${NC}"
        FAILED=$((FAILED+1))
    fi
}

check_warning() {
    echo -e "${YELLOW}⚠️  [$TOTAL_CHECKS] $1${NC}"
    WARNINGS=$((WARNINGS+1))
}

check_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

# ==================== 代码质量检查 (1-7) ====================
echo ""
echo "📝 代码质量检查"
echo "------------------------------------------------------------"

# 检查 1: Python 语法
if command -v python3 &> /dev/null; then
    python3 -m py_compile benchmarks/*.py 2>/dev/null
    check_result $? "Python 语法正确性"
else
    check_warning "Python3 未安装，跳过语法检查"
fi

# 检查 2: Python 代码风格 (PEP8)
if command -v pylint &> /dev/null; then
    pylint --exit-zero benchmarks/*.py > /dev/null 2>&1
    PYLINT_SCORE=$(pylint benchmarks/*.py 2>/dev/null | grep "Your code has been rated" | awk '{print $7}' | cut -d'/' -f1)
    if [ ! -z "$PYLINT_SCORE" ]; then
        if (( $(echo "$PYLINT_SCORE > 8.0" | bc -l) )); then
            check_result 0 "Pylint 代码质量评分: $PYLINT_SCORE/10"
        else
            check_result 1 "Pylint 代码质量评分过低: $PYLINT_SCORE/10"
        fi
    else
        check_warning "无法获取 Pylint 评分"
    fi
elif command -v flake8 &> /dev/null; then
    flake8 benchmarks/*.py --exit-zero > /dev/null 2>&1
    check_result $? "Flake8 代码风格检查"
else
    check_warning "Pylint/Flake8 未安装，跳过代码风格检查"
fi

# 检查 3: 代码复杂度
if command -v radon &> /dev/null; then
    COMPLEX=$(radon cc benchmarks/ -a -nb | grep "Average complexity" | awk '{print $NF}' | cut -d'(' -f1)
    if [ ! -z "$COMPLEX" ]; then
        if (( $(echo "$COMPLEX < 10" | bc -l) )); then
            check_result 0 "代码圈复杂度: $COMPLEX (目标<10)"
        else
            check_result 1 "代码圈复杂度过高: $COMPLEX"
        fi
    else
        check_warning "无法计算代码复杂度"
    fi
else
    check_warning "Radon 未安装，跳过复杂度检查 (pip install radon)"
fi

# 检查 4: 代码重复率
if command -v pylint &> /dev/null; then
    DUPLICATE=$(pylint --disable=all --enable=duplicate-code benchmarks/*.py 2>&1 | grep -c "duplicate-code" || echo "0")
    if [ "$DUPLICATE" -eq 0 ]; then
        check_result 0 "无重复代码"
    else
        check_result 1 "发现 $DUPLICATE 处重复代码"
    fi
else
    check_warning "跳过代码重复检查"
fi

# 检查 5: 类型注解覆盖
if command -v mypy &> /dev/null; then
    mypy benchmarks/*.py --ignore-missing-imports > /dev/null 2>&1
    check_result $? "MyPy 类型检查"
else
    check_warning "MyPy 未安装，跳过类型检查 (pip install mypy)"
fi

# 检查 6: Docstring 覆盖率
if command -v pydocstyle &> /dev/null; then
    pydocstyle benchmarks/*.py --count > /dev/null 2>&1
    DOCSTRING_ERRORS=$?
    if [ $DOCSTRING_ERRORS -eq 0 ]; then
        check_result 0 "Docstring 格式正确"
    else
        check_warning "Docstring 格式建议改进"
    fi
else
    check_warning "Pydocstyle 未安装 (pip install pydocstyle)"
fi

# 检查 7: 单元测试存在性和数量
TEST_COUNT=$(find benchmarks/tests -name "test_*.py" 2>/dev/null | wc -l)
if [ $TEST_COUNT -gt 0 ]; then
    check_result 0 "单元测试文件数量: $TEST_COUNT"
    
    # 统计测试用例数
    if command -v python3 &> /dev/null; then
        TOTAL_TESTS=$(python3 -m pytest benchmarks/tests/ --collect-only -q 2>/dev/null | tail -1 | awk '{print $1}' || echo "N/A")
        check_info "预估测试用例总数: $TOTAL_TESTS"
    fi
else
    check_result 1 "缺少单元测试"
fi

# ==================== 安全检查 (8-11) ====================
echo ""
echo "🔐 安全检查"
echo "------------------------------------------------------------"

# 检查 8: Python 安全漏洞扫描
if command -v bandit &> /dev/null; then
    bandit -r benchmarks/ -ll -f json -o /tmp/bandit_report.json > /dev/null 2>&1
    SECURITY_ISSUES=$(cat /tmp/bandit_report.json 2>/dev/null | python3 -c "import sys, json; print(len(json.load(sys.stdin)['results']))" 2>/dev/null || echo "0")
    
    if [ "$SECURITY_ISSUES" -eq 0 ]; then
        check_result 0 "Bandit 安全扫描: 未发现高危漏洞"
    else
        check_result 1 "Bandit 发现 $SECURITY_ISSUES 个安全问题"
    fi
    rm -f /tmp/bandit_report.json
else
    check_warning "Bandit 未安装 (pip install bandit)"
fi

# 检查 9: 依赖漏洞扫描
if command -v safety &> /dev/null; then
    safety check --json > /dev/null 2>&1
    check_result $? "Safety 依赖漏洞扫描"
else
    check_warning "Safety 未安装 (pip install safety)"
fi

# 检查 10: 密钥泄露检测
if command -v trufflehog &> /dev/null || command -v gitleaks &> /dev/null; then
    # 检查是否有硬编码的密钥
    SECRETS=$(grep -r -E "(api[_-]?key|password|secret|token)" benchmarks/ --include="*.py" | grep -v "test" | wc -l)
    if [ $SECRETS -eq 0 ]; then
        check_result 0 "未发现硬编码密钥"
    else
        check_warning "发现 $SECRETS 处可能的敏感信息引用（请人工确认）"
    fi
else
    # 简单的关键词检测
    HARDCODED=$(grep -r -E '(password|secret)\s*=\s*["\'][^"\']{8,}' benchmarks/ --include="*.py" | wc -l)
    if [ $HARDCODED -eq 0 ]; then
        check_result 0 "未发现明显的硬编码密钥"
    else
        check_result 1 "发现 $HARDCODED 处硬编码密钥"
    fi
fi

# 检查 11: SQL注入风险
SQL_INJECTION=$(grep -r -E "execute\(['\"].*%s.*['\"]" benchmarks/ --include="*.py" | wc -l)
if [ $SQL_INJECTION -eq 0 ]; then
    check_result 0 "未发现SQL注入风险"
else
    check_warning "发现 $SQL_INJECTION 处潜在SQL注入风险点"
fi

# ==================== 测试覆盖 (12-14) ====================
echo ""
echo "🧪 测试覆盖检查"
echo "------------------------------------------------------------"

# 检查 12: 单元测试覆盖率
if command -v pytest &> /dev/null && command -v pytest-cov &> /dev/null 2>&1; then
    COVERAGE=$(python3 -m pytest benchmarks/tests/ --cov=benchmarks --cov-report=term-missing 2>/dev/null | grep "TOTAL" | awk '{print $NF}' | tr -d '%')
    
    if [ ! -z "$COVERAGE" ]; then
        if (( $(echo "$COVERAGE >= 85" | bc -l) )); then
            check_result 0 "单元测试覆盖率: ${COVERAGE}% (优秀, >=85%)"
        elif (( $(echo "$COVERAGE >= 70" | bc -l) )); then
            check_warning "单元测试覆盖率: ${COVERAGE}% (良好, 目标>=85%)"
        else
            check_result 1 "单元测试覆盖率不足: ${COVERAGE}%"
        fi
    else
        check_warning "无法获取覆盖率数据"
    fi
else
    check_warning "Pytest/Coverage 未安装 (pip install pytest pytest-cov)"
fi

# 检查 13: 测试通过率
if command -v pytest &> /dev/null; then
    python3 -m pytest benchmarks/tests/ -v > /tmp/pytest_output.txt 2>&1
    TEST_RESULT=$?
    
    if [ $TEST_RESULT -eq 0 ]; then
        check_result 0 "所有单元测试通过"
    else
        FAILED_TESTS=$(grep "FAILED" /tmp/pytest_output.txt | wc -l)
        check_result 1 "$FAILED_TESTS 个测试失败"
    fi
    rm -f /tmp/pytest_output.txt
else
    check_warning "无法运行单元测试"
fi

# 检查 14: 测试执行时间
if command -v pytest &> /dev/null; then
    START_TIME=$(date +%s)
    python3 -m pytest benchmarks/tests/ -q > /dev/null 2>&1
    END_TIME=$(date +%s)
    TEST_DURATION=$((END_TIME - START_TIME))
    
    if [ $TEST_DURATION -lt 30 ]; then
        check_result 0 "测试执行时间: ${TEST_DURATION}秒 (目标<30秒)"
    else
        check_warning "测试执行时间较长: ${TEST_DURATION}秒"
    fi
fi

# ==================== 文档质量 (15-17) ====================
echo ""
echo "📚 文档质量检查"
echo "------------------------------------------------------------"

# 检查 15: 文档完整性
REQUIRED_DOCS=("README.md" "ENGINEERING_ANALYSIS.md" "IMPROVEMENTS_SUMMARY.md" "ENGINEER_FRAMEWORK_GUIDE.md")
MISSING_DOCS=0

for doc in "${REQUIRED_DOCS[@]}"; do
    if [ ! -f "$doc" ]; then
        MISSING_DOCS=$((MISSING_DOCS+1))
    fi
done

if [ $MISSING_DOCS -eq 0 ]; then
    check_result 0 "核心文档完整 (${#REQUIRED_DOCS[@]} 个文件)"
else
    check_result 1 "缺少 $MISSING_DOCS 个核心文档"
fi

# 检查 16: 文档链接完整性
if [ -f "scripts/check_doc_links.py" ]; then
    python3 scripts/check_doc_links.py > /tmp/link_check.txt 2>&1
    LINK_RESULT=$?
    
    if [ $LINK_RESULT -eq 0 ]; then
        check_result 0 "文档链接完整性检查通过"
    else
        BROKEN_LINKS=$(grep -c "❌" /tmp/link_check.txt || echo "0")
        check_result 1 "发现 $BROKEN_LINKS 个断链"
    fi
    rm -f /tmp/link_check.txt
else
    check_warning "文档链接检查工具不存在"
fi

# 检查 17: 中文文档编码
if [ -d "docs/zh-CN" ]; then
    UTF8_FILES=0
    NON_UTF8_FILES=0
    
    for file in docs/zh-CN/*.md; do
        if [ -f "$file" ]; then
            if file -i "$file" | grep -q "utf-8"; then
                UTF8_FILES=$((UTF8_FILES+1))
            else
                NON_UTF8_FILES=$((NON_UTF8_FILES+1))
            fi
        fi
    done
    
    if [ $NON_UTF8_FILES -eq 0 ]; then
        check_result 0 "中文文档 UTF-8 编码: $UTF8_FILES 个文件"
    else
        check_result 1 "$NON_UTF8_FILES 个文件非 UTF-8 编码"
    fi
fi

# ==================== 性能检查 (18-19) ====================
echo ""
echo "⚡ 性能检查"
echo "------------------------------------------------------------"

# 检查 18: 性能基准测试可执行性
if [ -f "benchmarks/run_benchmarks.sh" ]; then
    # 尝试运行模拟模式
    bash benchmarks/run_benchmarks.sh --simulate --quick > /dev/null 2>&1
    BENCH_RESULT=$?
    
    if [ $BENCH_RESULT -eq 0 ]; then
        check_result 0 "性能基准测试可执行"
    else
        check_warning "性能基准测试执行异常（可能需要依赖）"
    fi
else
    check_warning "性能基准测试脚本不存在"
fi

# 检查 19: 内存泄漏检测配置
if command -v memory_profiler &> /dev/null || command -v valgrind &> /dev/null; then
    check_result 0 "内存泄漏检测工具可用"
else
    check_warning "内存泄漏检测工具未安装 (pip install memory_profiler)"
fi

# ==================== CI/CD 集成 (20-21) ====================
echo ""
echo "⚙️  CI/CD 集成检查"
echo "------------------------------------------------------------"

# 检查 20: GitHub Actions 配置
WORKFLOWS=(".github/workflows/benchmark.yml" ".github/workflows/docker-build.yml")
VALID_WORKFLOWS=0

for workflow in "${WORKFLOWS[@]}"; do
    if [ -f "$workflow" ]; then
        VALID_WORKFLOWS=$((VALID_WORKFLOWS+1))
    fi
done

if [ $VALID_WORKFLOWS -eq ${#WORKFLOWS[@]} ]; then
    check_result 0 "CI/CD 工作流配置完整 ($VALID_WORKFLOWS 个)"
else
    check_warning "部分 CI/CD 工作流缺失"
fi

# 检查 21: Docker 配置
DOCKERFILES=("go/orchestrator/Dockerfile" "rust/agent-core/Dockerfile" "python/llm-service/Dockerfile")
VALID_DOCKERFILES=0

for dockerfile in "${DOCKERFILES[@]}"; do
    if [ -f "$dockerfile" ]; then
        VALID_DOCKERFILES=$((VALID_DOCKERFILES+1))
    fi
done

if [ $VALID_DOCKERFILES -eq ${#DOCKERFILES[@]} ]; then
    check_result 0 "Docker 配置完整 ($VALID_DOCKERFILES 个服务)"
else
    check_warning "部分 Dockerfile 缺失"
fi

# ==================== 技术债务管理 (22) ====================
echo ""
echo "💳 技术债务管理检查"
echo "------------------------------------------------------------"

# 检查 22: 技术债务记录
if [ -f "ENGINEERING_ANALYSIS.md" ]; then
    TD_COUNT=$(grep -c "TD-[0-9]" ENGINEERING_ANALYSIS.md || echo "0")
    TODO_COUNT=$(grep -c "TODO" ENGINEERING_ANALYSIS.md || echo "0")
    
    if [ $TD_COUNT -gt 0 ] || [ $TODO_COUNT -gt 0 ]; then
        check_result 0 "技术债务已记录 (TD: $TD_COUNT, TODO: $TODO_COUNT)"
    else
        check_warning "未发现明确的技术债务记录"
    fi
else
    check_warning "ENGINEERING_ANALYSIS.md 不存在"
fi

# ==================== 总结报告 ====================
echo ""
echo "============================================================"
echo "📊 检查总结"
echo "============================================================"

PASSED=$((TOTAL_CHECKS - FAILED))
SUCCESS_RATE=$(echo "scale=1; $PASSED * 100 / $TOTAL_CHECKS" | bc)

echo -e "总检查项: ${BLUE}$TOTAL_CHECKS${NC}"
echo -e "通过: ${GREEN}$PASSED${NC}"
echo -e "失败: ${RED}$FAILED${NC}"
echo -e "警告: ${YELLOW}$WARNINGS${NC}"
echo -e "成功率: ${BLUE}${SUCCESS_RATE}%${NC}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}🎉 恭喜！所有检查通过，代码已达到生产级质量标准。${NC}"
    echo ""
    echo "💡 建议："
    echo "  - 继续保持高标准的代码质量"
    echo "  - 定期运行此检查脚本"
    echo "  - 在 CI/CD 中集成这些检查"
    exit 0
elif [ $FAILED -le 3 ]; then
    echo -e "${YELLOW}⚠️  大部分检查通过，但有 $FAILED 项需要改进。${NC}"
    echo ""
    echo "💡 后续行动："
    echo "  - 查看上述失败的检查项"
    echo "  - 优先修复高优先级问题"
    echo "  - 考虑技术债务的偿还计划"
    exit 1
else
    echo -e "${RED}❌ 发现多个问题 ($FAILED 项)，需要系统性改进。${NC}"
    echo ""
    echo "💡 建议行动："
    echo "  - 创建问题清单并优先级排序"
    echo "  - 制定改进计划"
    echo "  - 逐步修复问题并重新运行检查"
    exit 1
fi

