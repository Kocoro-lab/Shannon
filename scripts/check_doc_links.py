#!/usr/bin/env python3
"""
Shannon 文档链接完整性检查工具
检查 Markdown 文档中的所有链接是否有效
"""

import re
import sys
from pathlib import Path
from typing import List, Tuple, Dict
from urllib.parse import urlparse

class DocLinkChecker:
    """文档链接检查器"""
    
    def __init__(self, root_dir: str = "."):
        self.root = Path(root_dir).resolve()
        self.errors = []
        self.warnings = []
        self.checked_files = 0
        self.total_links = 0
        
    def find_markdown_files(self) -> List[Path]:
        """查找所有 Markdown 文件"""
        return list(self.root.rglob("*.md"))
    
    def extract_links(self, content: str, file_path: Path) -> List[Tuple[str, int]]:
        """提取 Markdown 文件中的所有链接"""
        links = []
        
        # 匹配 [text](link) 格式的链接
        pattern = r'\[([^\]]+)\]\(([^)]+)\)'
        
        for line_num, line in enumerate(content.split('\n'), 1):
            matches = re.finditer(pattern, line)
            for match in matches:
                link = match.group(2)
                links.append((link, line_num))
        
        return links
    
    def check_local_link(self, link: str, source_file: Path) -> Tuple[bool, str]:
        """检查本地文件链接是否有效"""
        # 移除锚点
        link_path = link.split('#')[0]
        
        if not link_path:  # 仅锚点链接，跳过
            return True, ""
        
        # 计算绝对路径
        if link_path.startswith('/'):
            target = self.root / link_path.lstrip('/')
        else:
            target = (source_file.parent / link_path).resolve()
        
        if not target.exists():
            return False, f"文件不存在: {target.relative_to(self.root)}"
        
        return True, ""
    
    def check_url(self, url: str) -> Tuple[bool, str]:
        """检查 URL 格式是否有效（不进行网络请求）"""
        parsed = urlparse(url)
        
        if not parsed.scheme:
            return False, "缺少协议（http/https）"
        
        if not parsed.netloc:
            return False, "缺少域名"
        
        # 基本格式检查通过
        return True, ""
    
    def check_file(self, file_path: Path):
        """检查单个文件的所有链接"""
        try:
            content = file_path.read_text(encoding='utf-8')
        except Exception as e:
            self.errors.append(f"❌ {file_path.relative_to(self.root)}: 无法读取文件 - {e}")
            return
        
        links = self.extract_links(content, file_path)
        self.total_links += len(links)
        
        for link, line_num in links:
            # 跳过邮件链接
            if link.startswith('mailto:'):
                continue
            
            # 检查 URL
            if link.startswith('http://') or link.startswith('https://'):
                is_valid, msg = self.check_url(link)
                if not is_valid:
                    self.errors.append(
                        f"❌ {file_path.relative_to(self.root)}:{line_num} - "
                        f"无效的 URL: {link} ({msg})"
                    )
            
            # 检查本地文件链接
            else:
                is_valid, msg = self.check_local_link(link, file_path)
                if not is_valid:
                    self.errors.append(
                        f"❌ {file_path.relative_to(self.root)}:{line_num} - "
                        f"断链: [{link}] ({msg})"
                    )
        
        self.checked_files += 1
    
    def check_all(self):
        """检查所有 Markdown 文件"""
        print("🔍 Shannon 文档链接完整性检查")
        print("=" * 60)
        
        md_files = self.find_markdown_files()
        print(f"📄 找到 {len(md_files)} 个 Markdown 文件")
        print()
        
        for md_file in md_files:
            # 跳过 node_modules 等目录
            if 'node_modules' in str(md_file) or '.git' in str(md_file):
                continue
            
            self.check_file(md_file)
        
        self.print_summary()
    
    def print_summary(self):
        """打印检查摘要"""
        print()
        print("=" * 60)
        print("📊 检查摘要")
        print("=" * 60)
        print(f"✅ 已检查文件: {self.checked_files}")
        print(f"🔗 总链接数: {self.total_links}")
        print(f"❌ 错误数: {len(self.errors)}")
        print(f"⚠️  警告数: {len(self.warnings)}")
        print()
        
        if self.errors:
            print("❌ 发现以下错误:")
            print("-" * 60)
            for error in self.errors:
                print(error)
            print()
        
        if self.warnings:
            print("⚠️  发现以下警告:")
            print("-" * 60)
            for warning in self.warnings:
                print(warning)
            print()
        
        if not self.errors and not self.warnings:
            print("🎉 所有链接检查通过！")
            return 0
        elif self.errors:
            print("💔 发现断链或无效链接，请修复后再提交。")
            return 1
        else:
            print("⚠️  发现一些警告，建议检查。")
            return 0
    
    def check_specific_files(self, file_patterns: List[str]):
        """检查特定的文件"""
        files_to_check = []
        
        for pattern in file_patterns:
            path = Path(pattern)
            if path.is_file():
                files_to_check.append(path)
            elif path.is_dir():
                files_to_check.extend(path.rglob("*.md"))
            else:
                # 尝试作为通配符
                files_to_check.extend(self.root.glob(pattern))
        
        print(f"🔍 检查 {len(files_to_check)} 个文件...")
        print()
        
        for file in files_to_check:
            if file.suffix == '.md':
                self.check_file(file)
        
        return self.print_summary()


def main():
    """主函数"""
    import argparse
    
    parser = argparse.ArgumentParser(description="检查 Markdown 文档链接完整性")
    parser.add_argument(
        'files',
        nargs='*',
        help='要检查的文件或目录（默认检查所有）'
    )
    parser.add_argument(
        '--root',
        default='.',
        help='项目根目录（默认当前目录）'
    )
    
    args = parser.parse_args()
    
    checker = DocLinkChecker(root_dir=args.root)
    
    if args.files:
        exit_code = checker.check_specific_files(args.files)
    else:
        checker.check_all()
        exit_code = 1 if checker.errors else 0
    
    sys.exit(exit_code)


if __name__ == '__main__':
    main()

