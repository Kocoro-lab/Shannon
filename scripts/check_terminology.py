#!/usr/bin/env python3
"""
Shannon 中文文档术语一致性检查工具

用途：检测中文文档中不一致的术语使用
基准：docs/zh-CN/术语表.md
"""

import os
import re
import sys
from pathlib import Path
from collections import defaultdict

# 术语映射：错误术语 -> 正确术语
TERMINOLOGY_RULES = {
    # 核心概念
    '工作流程': '工作流',
    '智能体': '代理',
    '作业': '任务',
    '调度器': '编排器',
    '思考': '推理',  # 在Reasoning上下文中
    
    # 动词
    '提交任务': '提交任务',  # 正确，但检查是否有其他变体
    
    # 常见错误
    'agent': 'Agent',  # 应该保持大写或使用"代理"
}

# 需要检查的不一致模式
INCONSISTENCY_PATTERNS = [
    # 工作流相关
    (r'工作流程', '工作流'),
    (r'工作流\s*程序', '工作流'),
    
    # 代理相关  
    (r'智能体', '代理'),
    (r'智能代理', '代理'),
    
    # 任务相关
    (r'作业', '任务'),
    (r'工作', '任务'),  # 在某些上下文中
    
    # 编排相关
    (r'调度器', '编排器'),
    (r'协调器', '编排器'),
]


class TerminologyChecker:
    """术语一致性检查器"""
    
    def __init__(self, docs_dir="docs/zh-CN"):
        self.docs_dir = Path(docs_dir)
        self.issues = defaultdict(list)
        self.checked_files = 0
        self.total_issues = 0
    
    def check_file(self, filepath):
        """检查单个文件"""
        try:
            with open(filepath, 'r', encoding='utf-8') as f:
                lines = f.readlines()
        except Exception as e:
            print(f"⚠️  无法读取文件 {filepath}: {e}")
            return
        
        self.checked_files += 1
        file_issues = []
        
        for line_num, line in enumerate(lines, 1):
            # 跳过代码块
            if line.strip().startswith('```') or line.strip().startswith('    '):
                continue
            
            # 检查每个模式
            for pattern, correct_term in INCONSISTENCY_PATTERNS:
                if re.search(pattern, line):
                    issue = {
                        'line': line_num,
                        'content': line.strip(),
                        'incorrect': pattern,
                        'correct': correct_term,
                        'file': str(filepath)
                    }
                    file_issues.append(issue)
                    self.total_issues += 1
        
        if file_issues:
            self.issues[str(filepath)] = file_issues
    
    def check_all(self):
        """检查所有中文文档"""
        if not self.docs_dir.exists():
            print(f"❌ 目录不存在: {self.docs_dir}")
            return
        
        # 查找所有md文件
        md_files = list(self.docs_dir.glob("*.md"))
        
        # 排除术语表自身
        md_files = [f for f in md_files if f.name != '术语表.md']
        
        print(f"🔍 检查 {len(md_files)} 个中文文档...")
        print()
        
        for md_file in md_files:
            self.check_file(md_file)
        
        return self.generate_report()
    
    def generate_report(self):
        """生成检查报告"""
        print("=" * 60)
        print("📊 术语一致性检查报告")
        print("=" * 60)
        print()
        
        print(f"✅ 已检查文件: {self.checked_files}")
        print(f"{'🎉 未发现问题' if self.total_issues == 0 else f'⚠️  发现问题: {self.total_issues} 个'}")
        print()
        
        if self.total_issues == 0:
            print("🎉 所有文档术语使用一致！")
            return True
        
        # 按文件分组显示
        print("详细问题：")
        print()
        
        for filepath, issues in self.issues.items():
            filename = Path(filepath).name
            print(f"📄 {filename} ({len(issues)} 个问题)")
            print("-" * 60)
            
            for issue in issues[:5]:  # 最多显示5个
                print(f"  第 {issue['line']} 行:")
                print(f"    发现: \"{issue['incorrect']}\"")
                print(f"    建议: 使用 \"{issue['correct']}\"")
                print(f"    内容: {issue['content'][:80]}...")
                print()
            
            if len(issues) > 5:
                print(f"    ... 还有 {len(issues) - 5} 个问题")
                print()
        
        print("=" * 60)
        print("💡 修复建议:")
        print("=" * 60)
        print()
        print("1. 参考术语表: docs/zh-CN/术语表.md")
        print("2. 使用查找替换工具批量修正")
        print("3. 人工审查确保上下文正确")
        print()
        
        return False


def main():
    """主函数"""
    import argparse
    
    parser = argparse.ArgumentParser(
        description="Shannon 中文文档术语一致性检查"
    )
    parser.add_argument(
        "--dir",
        default="docs/zh-CN",
        help="中文文档目录（默认: docs/zh-CN）"
    )
    parser.add_argument(
        "--fix",
        action="store_true",
        help="自动修复（谨慎使用）"
    )
    
    args = parser.parse_args()
    
    checker = TerminologyChecker(docs_dir=args.dir)
    all_correct = checker.check_all()
    
    if args.fix and not all_correct:
        print()
        print("⚠️  自动修复功能尚未实现")
        print("   建议：人工审查并修复问题")
        sys.exit(1)
    
    # 返回状态码
    sys.exit(0 if all_correct else 1)


if __name__ == "__main__":
    main()


