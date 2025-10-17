# 技术债务GitHub Issues模板

> **用途**: 为识别的技术债务创建GitHub Issues  
> **原则**: 每项债务都有owner、优先级和偿还计划

---

## Issue 1: [Tech Debt] 修复基准测试套件

### 元信息
- **Labels**: `tech-debt`, `testing`, `high-priority`
- **Milestone**: v0.2.2
- **Assignee**: 待分配
- **Priority**: 🔴 P0 (Critical)
- **Estimated**: 2-3天

### 问题描述

**债务ID**: TD-005  
**类型**: 鲁莽且无意（Reckless & Inadvertent）  
**发现日期**: 2025-10-17

基准测试套件中64个测试，43个因API不匹配而失败，测试失败率67%。

**根本原因**:
1. 测试代码编写后从未实际运行验证
2. 测试期望的方法名与实现不符
3. 部分方法在实现中完全缺失

**影响**:
- 虚假的安全感（声称70%覆盖率，实际30%）
- 测试无法发挥质量保障作用
- 降低代码库可信度

### 失败测试列表

#### test_load_test.py (15/16 失败)
- `test_concurrent_users` - 方法不存在
- `test_constant_load` - 参数不匹配  
- `test_endurance_test` - 方法不存在
- `test_error_rate_calculation` - 方法不存在
- `test_latency_percentiles` - 方法不存在
- `test_ramp_up_load` - 参数不匹配
- `test_spike_load` - 方法名不匹配
- `test_stress_test` - 方法不存在
- 等等...

#### test_pattern_bench.py (12/14 失败)
- `test_chain_of_thought_pattern` - 方法名不匹配（test_* vs benchmark_*）
- `test_debate_pattern` - 方法名不匹配
- `test_pattern_comparison` - 方法不存在
- 等等...

#### test_tool_bench.py (13/14 失败)
- 所有test_*方法在ToolBenchmark类中不存在

#### test_workflow_bench.py (3/11 失败)
- `test_parallel_tasks` - 方法不存在
- `test_statistics_calculation` - 方法不存在
- `test_output_format` - 方法不存在

### 修复任务清单

- [ ] **阶段1**: 修复test_load_test.py (1天)
  - [ ] 统一类名和方法名
  - [ ] 实现缺失的辅助方法或删除相应测试
  - [ ] 验证所有测试通过

- [ ] **阶段2**: 修复test_pattern_bench.py (0.5天)
  - [ ] 统一方法名（test_* → benchmark_*）
  - [ ] 实现缺失的helper方法
  - [ ] 验证所有测试通过

- [ ] **阶段3**: 修复test_tool_bench.py (1天)
  - [ ] 重新设计测试策略或实现缺失方法
  - [ ] 验证所有测试通过

- [ ] **阶段4**: 修复test_workflow_bench.py (0.5天)
  - [ ] 实现run_parallel_tasks等方法
  - [ ] 验证所有测试通过

- [ ] **阶段5**: 验证整体 (0.5天)
  - [ ] 运行完整测试套件
  - [ ] 生成覆盖率报告
  - [ ] 达到70%+真实覆盖率

### 验收标准

- ✅ 所有64个测试通过
- ✅ 实际测试覆盖率≥70%
- ✅ CI中测试自动运行
- ✅ 文档已更新为实测数据

### 参考文档

- 详细分析: `docs/reviews/TEST_VALIDATION_FINDINGS.md`
- 修复追踪: `docs/reviews/FIXES_TRACKING.md`
- 审查报告: `docs/reviews/feat-performance-benchmarks-review.md`

---

## Issue 2: [Tech Debt] 文档同步流程自动化

### 元信息
- **Labels**: `tech-debt`, `documentation`, `low-priority`
- **Milestone**: v0.4.0
- **Assignee**: 待分配  
- **Priority**: 🟢 P2 (Low)
- **Estimated**: 3天

### 问题描述

**债务ID**: TD-003  
**类型**: 审慎且刻意（Prudent & Deliberate）

中英文文档需要手动同步，英文更新后中文可能滞后。

### 修复任务清单

- [ ] 为每个中文文档添加原文版本标记
- [ ] 创建GitHub Action检测英文文档更新
- [ ] 自动创建Issue提醒翻译团队
- [ ] 建立定期（每月）同步审查机制

### 验收标准

- ✅ 所有中文文档有版本标记
- ✅ GitHub Action配置并运行
- ✅ 测试同步检测有效性

---

## Issue 3: [Tech Debt] 真实集成测试集成到CI

### 元信息
- **Labels**: `tech-debt`, `testing`, `ci-cd`, `medium-priority`
- **Milestone**: v0.3.0
- **Assignee**: 待分配
- **Priority**: 🟡 P1 (Medium)
- **Estimated**: 1天

### 问题描述

**债务ID**: 新增  
**类型**: 审慎且刻意

基准测试工具支持真实模式，但CI中仅运行模拟模式。

### 修复任务清单

- [ ] 在GitHub Actions中启动Shannon服务
- [ ] 配置docker-compose for CI
- [ ] 添加真实集成测试步骤
- [ ] 配置合理的超时和重试

### 验收标准

- ✅ CI中运行真实集成测试
- ✅ 测试稳定（误报率<5%）
- ✅ 执行时间合理（<5分钟）

---

## Issue 4: [Enhancement] 统一中文文档术语

### 元信息
- **Labels**: `documentation`, `i18n`, `enhancement`
- **Milestone**: v0.2.2
- **Assignee**: 待分配（文档团队）
- **Priority**: 🟡 P1 (Medium)
- **Estimated**: 1天

### 问题描述

9个中文文档翻译分支使用了不一致的术语（如"工作流"vs"工作流程"，"代理"vs"智能体"）。

**已完成**:
- ✅ 创建了统一术语表 (`docs/zh-CN/术语表.md`)

### 修复任务清单

- [x] 创建术语表
- [ ] 检测所有中文文档的术语
- [ ] 人工修正不一致之处
- [ ] 创建术语检查脚本
- [ ] 添加CI检查

### 验收标准

- ✅ 所有中文文档术语符合术语表
- ✅ 术语检查脚本可用
- ✅ CI中自动检查术语一致性

---

## Issue 5: [RFC] 错误处理系统化改进实施

### 元信息
- **Labels**: `rfc`, `error-handling`, `epic`
- **Milestone**: Q1 2026
- **Assignee**: 待分配（后端团队）
- **Priority**: 🟡 P1 (Medium)
- **Estimated**: 4-5周（4个阶段）

### 问题描述

**RFC编号**: RFC-001  
**文档**: `docs/zh-CN/错误处理最佳实践.md`

系统性改进Shannon的错误处理和日志记录机制。

### Epic分解

#### Epic 1: 基础设施（1-2周）
- Issue #X1: 设计错误码体系
- Issue #X2: 实现ShannonError类型
- Issue #X3: 更新gRPC错误映射
- Issue #X4: 编写单元测试

#### Epic 2: 日志增强（1周）
- Issue #X5: 实现上下文日志记录
- Issue #X6: 添加性能日志
- Issue #X7: 实现敏感信息过滤
- Issue #X8: 创建日志分析工具

#### Epic 3: 监控集成（1周）
- Issue #X9: 添加错误指标
- Issue #X10: 配置告警规则
- Issue #X11: 创建错误仪表板
- Issue #X12: 集成到CI/CD

#### Epic 4: 文档和培训（1周）
- Issue #X13: 更新错误处理文档
- Issue #X14: 创建故障排除指南
- Issue #X15: 团队培训和最佳实践分享

### RFC决策流程

- **反馈期**: 2025-10-17 至 2025-10-25
- **决策日期**: 2025-10-30
- **实施启动**: 2025-11-01（如批准）

### 验收标准

- ✅ 团队评审并批准RFC
- ✅ 所有4个Epic完成
- ✅ 错误诊断时间减少70%
- ✅ 错误分类准确率>90%

---

## 使用指南

### 如何创建这些Issues

```bash
# 1. 在GitHub Web界面创建Issue
# 2. 复制上述模板内容
# 3. 填写Assignee和Milestone
# 4. 添加Labels
# 5. 关联到项目看板

# 或使用GitHub CLI
gh issue create \
  --title "[Tech Debt] 修复基准测试套件" \
  --body-file issue_td005.md \
  --label "tech-debt,testing,high-priority" \
  --milestone "v0.2.2"
```

### Issue关联

在相关PR或commit中引用Issue:
```
Addresses #123  # 与Issue相关
Resolves #124   # 完全解决Issue
Related to #125 # 相关Issue
```

---

**模板版本**: v1.0  
**创建日期**: 2025年10月17日  
**维护者**: Engineering Team


