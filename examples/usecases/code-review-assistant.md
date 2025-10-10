# 代码审查助手示例

这个示例展示如何使用 Shannon 构建一个智能代码审查系统。

## 场景

自动审查 Pull Request，识别潜在问题，提供改进建议，并生成审查报告。

## 工作流程

```
代码分析 → 安全检查 → 性能评估 → 最佳实践验证 → 生成报告 → 建议修改
```

## 使用的 Shannon 功能

- **Debate Pattern**: 多角度审查代码
- **ReAct Pattern**: 迭代发现和修复问题
- **Reflection Pattern**: 确保审查质量
- **Python Executor**: 运行代码质量检查
- **Web Search Tool**: 查找最佳实践和安全建议

## 示例代码

### 1. 提交代码审查任务

```bash
./scripts/submit_task.sh "审查以下 Python 代码：

\`\`\`python
def process_user_data(user_input):
    # 从数据库获取用户
    query = f\"SELECT * FROM users WHERE id = {user_input}\"
    result = db.execute(query)
    
    # 处理结果
    if result:
        return result[0]
    return None

def calculate_total(items):
    total = 0
    for item in items:
        total = total + item['price']
    return total
\`\`\`

请检查：
1. 安全漏洞
2. 性能问题
3. 代码质量
4. 最佳实践
5. 提供改进建议"
```

### 2. 使用 Debate Pattern 进行多角度审查

Shannon 会创建多个专业代理：

```
代理 1 (安全专家):
❌ SQL 注入漏洞！
使用字符串格式化构建 SQL 查询是严重的安全风险。
建议：使用参数化查询

代理 2 (性能专家):
⚠️ 性能问题
calculate_total 使用低效的字符串连接
建议：使用 sum() 和生成器表达式

代理 3 (代码质量专家):
⚠️ 缺少错误处理
没有处理数据库连接失败的情况
建议：添加 try-except 块
```

### 3. 生成改进代码

```python
# Shannon 使用 Python Executor 验证改进后的代码

def process_user_data(user_input):
    """
    安全地获取用户数据
    
    Args:
        user_input: 用户ID（字符串或整数）
    
    Returns:
        用户数据字典，未找到则返回 None
        
    Raises:
        DatabaseError: 数据库连接失败
    """
    try:
        # ✅ 使用参数化查询防止 SQL 注入
        query = "SELECT * FROM users WHERE id = ?"
        result = db.execute(query, (user_input,))
        
        # ✅ 明确的返回值处理
        return result[0] if result else None
        
    except db.DatabaseError as e:
        # ✅ 适当的错误处理和日志
        logger.error(f"Database error: {e}")
        raise
    except Exception as e:
        # ✅ 捕获意外错误
        logger.error(f"Unexpected error: {e}")
        return None

def calculate_total(items):
    """
    计算商品总价
    
    Args:
        items: 商品列表，每个商品包含 'price' 键
        
    Returns:
        总价（浮点数）
    """
    # ✅ 使用内置函数和生成器（高效）
    return sum(item.get('price', 0) for item in items)
```

## API 调用示例

### 审查整个 Pull Request

```python
import grpc
from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc

channel = grpc.insecure_channel('localhost:50052')
client = orchestrator_pb2_grpc.OrchestratorServiceStub(channel)

# 读取 PR 差异
with open('pr_diff.txt', 'r') as f:
    pr_diff = f.read()

# 提交审查任务
task = orchestrator_pb2.TaskRequest(
    query=f"""
    作为高级代码审查员，请全面审查以下 Pull Request：
    
    {pr_diff}
    
    审查重点：
    1. 安全漏洞（SQL注入、XSS、CSRF等）
    2. 性能问题（N+1查询、内存泄漏等）
    3. 代码质量（可读性、可维护性、测试覆盖率）
    4. 架构设计（SOLID原则、设计模式）
    5. 错误处理和日志记录
    6. 文档和注释
    
    对于每个问题：
    - 标记严重程度（Critical/High/Medium/Low）
    - 提供具体位置（文件:行号）
    - 解释问题原因
    - 提供修复建议和示例代码
    """,
    mode="debate",  # 使用辩论模式获得多角度审查
    tools=["python_executor", "web_search"],
    session_id="code_review_pr_123",
    metadata={
        "pr_number": "123",
        "repository": "myorg/myrepo",
        "author": "developer@example.com",
        "branch": "feature/new-api"
    }
)

response = client.SubmitTask(task)
print(f"审查任务已提交: {response.workflow_id}")

# 流式获取审查进度
for update in client.StreamTaskExecution(
    orchestrator_pb2.StreamRequest(workflow_id=response.workflow_id)
):
    if update.type == "AGENT_COMPLETED":
        print(f"✓ {update.agent_id} 完成审查")
```

### 自动化 CI/CD 集成

```yaml
# .github/workflows/shannon-review.yml
name: Shannon Code Review

on:
  pull_request:
    types: [opened, synchronize]

jobs:
  shannon-review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      
      - name: Get PR diff
        run: |
          git diff origin/main...HEAD > pr_diff.txt
      
      - name: Submit to Shannon
        run: |
          ./scripts/submit_code_review.sh pr_diff.txt
        env:
          SHANNON_API_KEY: ${{ secrets.SHANNON_API_KEY }}
      
      - name: Post review comments
        uses: actions/github-script@v6
        with:
          script: |
            const review = require('./shannon_review.json');
            // 发布审查评论到 PR
```

## 审查报告示例

```markdown
# 代码审查报告 - PR #123

**审查时间**: 2025-01-10 14:30:00  
**审查者**: Shannon AI (Debate Pattern)  
**代码变更**: +245 -120 行

## 执行摘要

- 🔴 Critical 问题: 2
- 🟡 High 问题: 5
- 🔵 Medium 问题: 8
- ⚪ Low 问题: 12

**总体评分**: 6.5/10  
**建议**: 修复 Critical 和 High 问题后可合并

---

## Critical 问题

### 🔴 SQL 注入漏洞
**位置**: `src/api/users.py:45`  
**严重程度**: Critical

```python
# ❌ 问题代码
query = f"SELECT * FROM users WHERE id = {user_id}"
```

**问题说明**:
直接将用户输入插入 SQL 查询，可能导致 SQL 注入攻击。
攻击者可以输入 `1 OR 1=1` 获取所有用户数据。

**修复建议**:
```python
# ✅ 修复后
query = "SELECT * FROM users WHERE id = ?"
result = db.execute(query, (user_id,))
```

**参考**:
- [OWASP SQL Injection](https://owasp.org/www-community/attacks/SQL_Injection)
- [Python DB-API](https://peps.python.org/pep-0249/)

---

### 🔴 敏感信息泄漏
**位置**: `src/config/settings.py:12`  
**严重程度**: Critical

```python
# ❌ 问题代码
API_KEY = "sk_live_1234567890abcdef"  # 硬编码密钥
```

**问题说明**:
API 密钥硬编码在源代码中，将被提交到版本控制系统。

**修复建议**:
```python
# ✅ 修复后
import os
API_KEY = os.getenv('API_KEY')
if not API_KEY:
    raise ValueError("API_KEY environment variable not set")
```

---

## High 问题

### 🟡 N+1 查询问题
**位置**: `src/api/orders.py:78`  
**严重程度**: High

```python
# ❌ 问题代码
for order in orders:
    order.user = User.query.get(order.user_id)  # 每次循环一次查询
```

**性能影响**:
100 个订单将执行 101 次数据库查询（1 + 100）

**修复建议**:
```python
# ✅ 修复后 - 使用 JOIN 或预加载
orders = Order.query.options(joinedload(Order.user)).all()
```

**预计性能提升**: 95% 减少数据库往返

---

## Medium 问题

### 🔵 缺少错误处理
**位置**: `src/utils/file_handler.py:23`

```python
# ❌ 问题代码
def read_config(filename):
    with open(filename, 'r') as f:
        return json.load(f)
```

**修复建议**:
```python
# ✅ 修复后
def read_config(filename):
    try:
        with open(filename, 'r') as f:
            return json.load(f)
    except FileNotFoundError:
        logger.error(f"Config file not found: {filename}")
        return {}
    except json.JSONDecodeError as e:
        logger.error(f"Invalid JSON in {filename}: {e}")
        return {}
```

---

## 积极方面 ✨

1. ✅ **良好的测试覆盖率**: 新增代码的测试覆盖率达到 85%
2. ✅ **清晰的命名**: 变量和函数命名符合 PEP 8 规范
3. ✅ **文档完善**: 所有公共 API 都有 docstring
4. ✅ **类型提示**: 使用了 Python 类型提示提高代码可读性

---

## 建议的修改优先级

### 立即修复 (阻塞合并)
1. SQL 注入漏洞 - `users.py:45`
2. 敏感信息泄漏 - `settings.py:12`

### 本周修复 (高优先级)
3. N+1 查询问题 - `orders.py:78`
4. 缺少输入验证 - `api/endpoints.py:102`
5. 错误处理缺失 - `utils/file_handler.py:23`

### 改进建议 (可选)
6. 添加更多单元测试
7. 改进错误消息的可读性
8. 考虑使用缓存优化性能

---

## 自动化检查结果

✅ 代码格式化 (black): 通过  
✅ 代码检查 (flake8): 通过  
⚠️ 类型检查 (mypy): 3 个警告  
✅ 安全扫描 (bandit): 2 个 High 问题  
✅ 测试通过率: 100% (42/42)  

---

## 下一步行动

1. [ ] 修复 2 个 Critical 问题
2. [ ] 修复 5 个 High 问题
3. [ ] 更新相关文档
4. [ ] 重新运行安全扫描
5. [ ] 请求重新审查

**预计修复时间**: 2-4 小时
```

## 配置优化

### 针对不同语言的审查

```yaml
# config/shannon.yaml
code_review:
  python:
    tools:
      - pylint
      - black
      - mypy
      - bandit
    max_file_size: 5000  # 行
  
  javascript:
    tools:
      - eslint
      - prettier
    max_file_size: 3000
  
  go:
    tools:
      - golint
      - gofmt
    max_file_size: 4000

patterns:
  debate:
    num_agents: 3  # 安全、性能、质量专家
    max_rounds: 2
```

### 审查严格程度

```yaml
review_levels:
  strict:
    block_on: ["critical", "high"]
    require_tests: true
    min_coverage: 80
  
  moderate:
    block_on: ["critical"]
    require_tests: false
    min_coverage: 60
  
  lenient:
    block_on: []
    require_tests: false
    min_coverage: 0
```

## 扩展功能

### 1. 安全扫描集成

```python
# 结合外部安全工具
def security_scan(code):
    # 使用 Shannon + Bandit/Semgrep
    shannon_review = submit_task(f"安全审查: {code}")
    bandit_results = run_bandit(code)
    
    # 合并结果
    return merge_security_findings(shannon_review, bandit_results)
```

### 2. 智能修复建议

```python
# 让 Shannon 直接生成修复补丁
task = f"""
审查代码并生成 git diff 格式的修复补丁：
{code}

要求：
1. 修复所有安全问题
2. 优化性能瓶颈
3. 保持功能不变
4. 生成标准 diff 格式
"""
```

### 3. 学习型审查

```python
# 从过往审查中学习
metadata = {
    "past_reviews": ["review_001", "review_002"],
    "team_style_guide": "https://company.com/style-guide",
    "common_patterns": load_team_patterns()
}
```

## 性能优化

### 大型 PR 的处理

```python
# 分批审查大型 PR
def review_large_pr(pr_files):
    # 按模块分组
    modules = group_by_module(pr_files)
    
    # 并行审查各模块
    reviews = []
    for module in modules:
        review = submit_task(
            f"审查模块: {module}",
            mode="parallel"
        )
        reviews.append(review)
    
    # 汇总结果
    return merge_reviews(reviews)
```

## 扩展阅读

- [Debate 模式使用指南](../../docs/zh-CN/模式使用指南.md#辩论模式)
- [Python 代码执行文档](../../docs/zh-CN/Python代码执行.md)
- [添加自定义工具](../../docs/zh-CN/添加自定义工具.md)

---

*示例更新：2025年1月*

