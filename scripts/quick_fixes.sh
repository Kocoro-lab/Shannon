#!/bin/bash
# Shannon PR审查 - 快速修复脚本
# 基于首席工程师框架审查发现的问题

set -e

echo "🔧 Shannon PR审查快速修复脚本"
echo "========================================"
echo ""

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# 检查当前分支
CURRENT_BRANCH=$(git branch --show-current)
echo "当前分支: $CURRENT_BRANCH"
echo ""

# 1. 验证测试覆盖率（feat/performance-benchmarks）
if [ "$CURRENT_BRANCH" == "feat/performance-benchmarks" ]; then
    echo "📊 修复1: 验证测试覆盖率"
    echo "----------------------------------------"
    
    if command -v pytest &> /dev/null; then
        echo "正在运行测试并生成覆盖率报告..."
        
        if pytest benchmarks/tests/ --cov=benchmarks --cov-report=term --cov-report=html 2>&1 | tee coverage_output.txt; then
            print_success "测试覆盖率报告已生成"
            
            # 提取覆盖率数据
            COVERAGE=$(grep "TOTAL" coverage_output.txt | awk '{print $NF}')
            echo "实测覆盖率: $COVERAGE"
            
            # 保存到文件供后续使用
            echo "COVERAGE=$COVERAGE" > .coverage_data
            print_success "覆盖率数据已保存到 .coverage_data"
            
        else
            print_warning "测试运行失败，可能缺少依赖"
            echo "安装依赖: pip install pytest pytest-cov"
        fi
    else
        print_warning "pytest未安装，跳过覆盖率检查"
        echo "安装: pip install pytest pytest-cov"
    fi
    echo ""
fi

# 2. 修复文档编码问题（docs分支）
echo "📝 修复2: 检查并修复文档编码"
echo "----------------------------------------"

if [ -d "docs/zh-CN" ]; then
    UTF16_FILES=0
    FIXED_FILES=0
    
    for file in docs/zh-CN/*.md; do
        if [ -f "$file" ]; then
            # 检查编码
            ENCODING=$(file -b --mime-encoding "$file")
            
            if [ "$ENCODING" == "utf-16le" ] || [ "$ENCODING" == "utf-16be" ]; then
                echo "发现UTF-16文件: $file"
                UTF16_FILES=$((UTF16_FILES + 1))
                
                # 转换为UTF-8
                if command -v iconv &> /dev/null; then
                    iconv -f UTF-16 -t UTF-8 "$file" > "$file.tmp"
                    mv "$file.tmp" "$file"
                    FIXED_FILES=$((FIXED_FILES + 1))
                    print_success "已转换: $file → UTF-8"
                else
                    print_warning "iconv不可用，无法转换"
                fi
            fi
        fi
    done
    
    if [ $UTF16_FILES -eq 0 ]; then
        print_success "所有中文文档都是UTF-8编码"
    else
        print_success "已转换 $FIXED_FILES 个文件"
    fi
else
    print_warning "docs/zh-CN目录不存在"
fi
echo ""

# 3. 检查文档链接
echo "🔗 修复3: 检查文档链接"
echo "----------------------------------------"

if [ -f "scripts/check_doc_links.py" ]; then
    if command -v python3 &> /dev/null; then
        python3 scripts/check_doc_links.py
    else
        print_warning "Python3不可用，跳过链接检查"
    fi
else
    print_warning "check_doc_links.py不存在"
fi
echo ""

# 4. 运行真实集成测试（如果Shannon服务正在运行）
echo "🧪 修复4: 尝试运行真实集成测试"
echo "----------------------------------------"

if docker ps | grep -q "shannon"; then
    print_success "检测到Shannon服务正在运行"
    
    if [ -f "benchmarks/workflow_bench.py" ]; then
        echo "运行真实工作流测试（10个请求）..."
        
        if python3 benchmarks/workflow_bench.py --endpoint localhost:50052 --requests 10 --output real_test_results.json 2>&1; then
            print_success "真实测试完成"
            echo "结果已保存到: real_test_results.json"
        else
            print_warning "测试失败，可能服务未完全启动或端口不匹配"
        fi
    fi
else
    print_warning "Shannon服务未运行，跳过集成测试"
    echo "启动命令: docker-compose up -d"
fi
echo ""

# 5. 生成修复报告
echo "📋 生成修复报告"
echo "----------------------------------------"

REPORT_FILE="quick_fixes_report_$(date +%Y%m%d_%H%M%S).md"

cat > "$REPORT_FILE" << EOF
# PR审查快速修复报告

> **执行时间**: $(date "+%Y-%m-%d %H:%M:%S")  
> **分支**: $CURRENT_BRANCH

## 执行的修复

### 1. 测试覆盖率验证
EOF

if [ -f ".coverage_data" ]; then
    source .coverage_data
    echo "- ✅ 已运行测试并生成覆盖率报告" >> "$REPORT_FILE"
    echo "- 实测覆盖率: $COVERAGE" >> "$REPORT_FILE"
    echo "- 报告位置: \`htmlcov/index.html\`" >> "$REPORT_FILE"
else
    echo "- ⚠️  未执行（pytest不可用或测试失败）" >> "$REPORT_FILE"
fi

cat >> "$REPORT_FILE" << EOF

### 2. 文档编码修复
EOF

if [ -d "docs/zh-CN" ]; then
    echo "- ✅ 已检查docs/zh-CN目录" >> "$REPORT_FILE"
    echo "- 发现UTF-16文件: $UTF16_FILES 个" >> "$REPORT_FILE"
    echo "- 已修复: $FIXED_FILES 个" >> "$REPORT_FILE"
else
    echo "- ⏭️  跳过（目录不存在）" >> "$REPORT_FILE"
fi

cat >> "$REPORT_FILE" << EOF

### 3. 文档链接检查
- 已执行check_doc_links.py（如可用）

### 4. 真实集成测试
EOF

if [ -f "real_test_results.json" ]; then
    echo "- ✅ 已运行真实集成测试" >> "$REPORT_FILE"
    echo "- 结果文件: \`real_test_results.json\`" >> "$REPORT_FILE"
else
    echo "- ⚠️  未执行（Shannon服务未运行）" >> "$REPORT_FILE"
fi

cat >> "$REPORT_FILE" << EOF

## 后续行动

### 立即
- [ ] 查看覆盖率报告: \`open htmlcov/index.html\`
- [ ] 查看真实测试结果: \`cat real_test_results.json\`
- [ ] 更新COMPLETION_REPORT.md中的覆盖率数据

### 下一步
- [ ] 提交修复: \`git add . && git commit -m "fix: apply PR review fixes"\`
- [ ] 更新PR描述，说明已完成的修复

## 参考
- PR审查报告: \`docs/reviews/\`
- 总览报告: \`docs/reviews/ALL_BRANCHES_REVIEW_SUMMARY.md\`
EOF

print_success "修复报告已生成: $REPORT_FILE"
echo ""

# 6. 总结
echo "✅ 快速修复完成！"
echo "========================================"
echo ""
echo "📊 修复总结:"
echo "  - 测试覆盖率: $([ -f '.coverage_data' ] && echo '✅ 已验证' || echo '⚠️  待验证')"
echo "  - 文档编码: $([ -d 'docs/zh-CN' ] && echo "✅ 已检查（修复$FIXED_FILES个）" || echo '⏭️  跳过')"
echo "  - 文档链接: $([ -f 'scripts/check_doc_links.py' ] && echo '✅ 已检查' || echo '⏭️  跳过')"
echo "  - 集成测试: $([ -f 'real_test_results.json' ] && echo '✅ 已运行' || echo '⚠️  待运行')"
echo ""
echo "📋 详细报告: $REPORT_FILE"
echo ""
echo "💡 提示:"
echo "  1. 查看覆盖率: open htmlcov/index.html"
echo "  2. 查看审查报告: ls docs/reviews/"
echo "  3. 提交修复: git add . && git commit -m 'fix: apply PR review fixes'"


