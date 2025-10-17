"""
基准测试配置
集中管理所有配置常量，避免魔法数字
"""

# ============================================
# 超时设置 (秒)
# ============================================

# 简单任务应在30秒内完成
SIMPLE_TASK_TIMEOUT = 30.0

# 复杂任务（如Tree-of-Thoughts）允许2分钟
COMPLEX_TASK_TIMEOUT = 120.0

# DAG工作流超时（每个子任务）
DAG_SUBTASK_TIMEOUT = 60.0


# ============================================
# 模拟延迟设置 (秒)
# 基于真实系统的P50延迟估算
# ============================================

SIMULATION_DELAYS = {
    'simple_task': 0.5,      # 简单任务基线
    'cot': 1.5,              # Chain-of-Thought 需要多轮推理
    'react': 2.0,            # ReAct 需要工具调用
    'debate': 4.5,           # Debate 需要多个agent交互
    'tot': 3.5,              # Tree-of-Thoughts 需要分支探索
    'reflection': 2.5,       # Reflection 需要自我审查
    'dag_subtask': 0.3,      # DAG子任务单位延迟
}


# ============================================
# 默认测试参数
# ============================================

# 简单任务基准测试
DEFAULT_SIMPLE_REQUESTS = 100
DEFAULT_CONCURRENCY = 10

# DAG工作流测试
DEFAULT_DAG_REQUESTS = 20
DEFAULT_DAG_SUBTASKS = 5

# 模式基准测试
DEFAULT_PATTERN_REQUESTS = 5


# ============================================
# 性能目标和阈值
# ============================================

# 简单任务性能目标
SIMPLE_TASK_TARGETS = {
    'p50': 0.5,   # P50应 < 500ms
    'p95': 2.0,   # P95应 < 2s
    'p99': 5.0,   # P99应 < 5s
    'throughput': 100,  # 吞吐量 > 100 req/s
}

# DAG工作流性能目标
DAG_WORKFLOW_TARGETS = {
    'p50': 5.0,   # P50应 < 5s
    'p95': 30.0,  # P95应 < 30s
    'p99': 60.0,  # P99应 < 60s
    'throughput': 10,  # 吞吐量 > 10 req/s
}


# ============================================
# 统计计算
# ============================================

def safe_percentile(sorted_list, percentile):
    """
    安全计算百分位数，处理边界情况
    
    Args:
        sorted_list: 已排序的列表
        percentile: 百分位数 (0.0-1.0)
    
    Returns:
        百分位数值，如果列表为空则返回None
    
    Examples:
        >>> safe_percentile([1, 2, 3, 4, 5], 0.5)   # P50
        3
        >>> safe_percentile([1], 0.95)              # 单元素
        1
        >>> safe_percentile([], 0.95)               # 空列表
        None
    """
    if not sorted_list:
        return None
    
    if len(sorted_list) == 1:
        return sorted_list[0]
    
    # 计算索引，确保不越界
    index = min(
        int(len(sorted_list) * percentile),
        len(sorted_list) - 1
    )
    
    return sorted_list[index]


# ============================================
# gRPC配置
# ============================================

DEFAULT_GRPC_ENDPOINT = "localhost:50052"
DEFAULT_API_KEY = "test-key"


# ============================================
# 输出配置
# ============================================

# 日志级别
LOG_LEVEL = "INFO"  # DEBUG, INFO, WARNING, ERROR

# 结果输出格式
OUTPUT_FORMAT = "json"  # json, csv, text

# 报告生成配置
REPORT_CONFIG = {
    'include_charts': True,
    'chart_format': 'png',
    'chart_size': (1200, 800),
}


# ============================================
# 验证函数
# ============================================

def validate_config():
    """验证配置的合理性"""
    errors = []
    
    # 检查超时时间为正数
    if SIMPLE_TASK_TIMEOUT <= 0:
        errors.append("SIMPLE_TASK_TIMEOUT must be positive")
    
    if COMPLEX_TASK_TIMEOUT <= SIMPLE_TASK_TIMEOUT:
        errors.append("COMPLEX_TASK_TIMEOUT should be greater than SIMPLE_TASK_TIMEOUT")
    
    # 检查模拟延迟
    for key, delay in SIMULATION_DELAYS.items():
        if delay < 0:
            errors.append(f"SIMULATION_DELAYS['{key}'] must be non-negative")
    
    # 检查并发数
    if DEFAULT_CONCURRENCY <= 0:
        errors.append("DEFAULT_CONCURRENCY must be positive")
    
    if errors:
        raise ValueError("Configuration errors:\n" + "\n".join(f"  - {e}" for e in errors))
    
    return True


# 启动时验证配置
if __name__ == "__main__":
    validate_config()
    print("✅ Configuration is valid")
    print(f"\nSimulation delays:")
    for pattern, delay in sorted(SIMULATION_DELAYS.items()):
        print(f"  {pattern:15s}: {delay:.1f}s")
    
    print(f"\nPerformance targets (simple tasks):")
    for metric, value in SIMPLE_TASK_TARGETS.items():
        unit = "req/s" if metric == "throughput" else "s"
        print(f"  {metric:10s}: {value:6.1f}{unit}")


