# Shannon 流式 API

本文档描述编排器公开的最小化、确定性流式接口。它涵盖 gRPC、服务器发送事件（SSE）和 WebSocket（WS）端点，包括过滤器和恢复语义以重新加入会话。

## 事件模型

- 字段：`workflow_id`、`type`、`agent_id?`、`message?`、`timestamp`、`seq`。
- 最小事件类型（位于 `streaming_v1` 门控后）：
  - `WORKFLOW_STARTED`、`AGENT_STARTED`、`AGENT_COMPLETED`、`ERROR_OCCURRED`。
  - P2P v1 添加：`MESSAGE_SENT`、`MESSAGE_RECEIVED`、`WORKSPACE_UPDATED`。
- 确定性：事件从工作流作为活动发出，记录在 Temporal 历史中，并发布到本地流管理器。

## gRPC: StreamingService

- RPC：`StreamingService.StreamTaskExecution(StreamRequest) returns (stream TaskUpdate)`
- 请求字段：
  - `workflow_id`（必需）
  - `types[]`（可选）— 按事件类型过滤
  - `last_event_id`（可选）— 使用 `seq > last_event_id` 的事件恢复
- 响应：`TaskUpdate` 镜像事件模型。

示例（伪 Go 代码）：

```go
client := pb.NewStreamingServiceClient(conn)
stream, _ := client.StreamTaskExecution(ctx, &pb.StreamRequest{
    WorkflowId: wfID,
    Types:      []string{"AGENT_STARTED", "AGENT_COMPLETED"},
    LastEventId: 42,
})
for {
    upd, err := stream.Recv()
    if err != nil { break }
    fmt.Println(upd.Type, upd.AgentId, upd.Seq)
}
```

## SSE: HTTP `/stream/sse`

- 方法：`GET /stream/sse?workflow_id=<id>&types=<csv>&last_event_id=<n>`
- 头部：支持 `Last-Event-ID` 用于浏览器自动恢复。
- CORS：`Access-Control-Allow-Origin: *`（开发友好；前门应在生产中强制认证）。

示例（curl）：

```bash
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=AGENT_STARTED,AGENT_COMPLETED"
```

注意：
- 服务器发出 `id: <seq>`，以便浏览器可以使用 `Last-Event-ID` 重新连接。
- 每 15 秒发送心跳作为 SSE 注释以保持代理存活。

## WebSocket: HTTP `/stream/ws`

- 方法：`GET /stream/ws?workflow_id=<id>&types=<csv>&last_event_id=<n>`
- 消息：与事件模型匹配的 JSON 对象。
- 心跳：服务器每约 20 秒 ping 一次；客户端应回复 pong。

示例（JS）：

```js
const ws = new WebSocket(`ws://localhost:8081/stream/ws?workflow_id=${wf}`);
ws.onmessage = (e) => {
  const evt = JSON.parse(e.data); // {workflow_id,type,agent_id,message,timestamp,seq}
};
```

## 无效工作流检测

gRPC 和 SSE 流式端点都会自动验证工作流是否存在，以快速失败无效的工作流 ID：

### 行为

- **验证超时**：从连接开始 30 秒
- **验证方法**：使用 Temporal `DescribeWorkflowExecution` API
- **首次事件计时器**：如果 30 秒内未收到事件则触发

### 按传输方式的响应

**gRPC (`StreamingService.StreamTaskExecution`)**
- 返回 `NotFound` gRPC 错误代码
- 错误消息：`"workflow not found"` 或 `"workflow not found or unavailable"`

**SSE (`/stream/sse`)**
- 在关闭前发出 `ERROR_OCCURRED` 事件：
  ```
  event: ERROR_OCCURRED
  data: {"workflow_id":"xxx","type":"ERROR_OCCURRED","message":"Workflow not found"}
  ```
- 等待时每 10 秒包含心跳 ping（`: ping`）

**WebSocket (`/stream/ws`)**
- 与 SSE 相同的行为，发送 JSON 错误事件然后关闭连接

### 有效工作流的边界情况

- **工作流存在但在 30 秒内不产生事件**：流保持打开，计时器重置
- **验证期间 Temporal 不可用**：立即返回错误
- **有效工作流**：首个事件到达后计时器被禁用

### 使用示例

```bash
# 无效工作流 - 约 30 秒后返回错误
shannon stream "invalid-workflow-123"
# 30 秒后输出：
# ERROR_OCCURRED: Workflow not found

# 有效工作流 - 正常流式传输
shannon stream "task-user-1234567890"
# 输出：立即流式传输事件
```

### 注意事项

- 这可以防止在流式传输不存在的工作流时无限期挂起
- 30 秒超时在响应性与允许慢速工作流启动之间取得平衡
- 心跳在验证期间通过代理保持连接存活

## 动态团队（信号）+ 团队事件

当 `SupervisorWorkflow` 中启用 `dynamic_team_v1` 时，工作流接受信号：

- 招募：信号名称 `recruit_v1`，JSON 为 `{ "Description": string, "Role"?: string }`。
- 退役：信号名称 `retire_v1`，JSON 为 `{ "AgentID": string }`。

授权操作发出流式事件：

- `TEAM_RECRUITED`，`agent_id` 为角色（对于最小 v1），`message` 为描述。
- `TEAM_RETIRED`，`agent_id` 为退役的代理。

通过 docker compose 内的 Temporal CLI 发送信号的辅助脚本：

```bash
# 为子任务招募新工作者
./scripts/signal_team.sh recruit <WORKFLOW_ID> "Summarize section 3" writer

# 退役工作者
./scripts/signal_team.sh retire <WORKFLOW_ID> agent-xyz
```

提示：使用 SSE/WS 过滤器仅观察团队事件：

```bash
curl -N "http://localhost:8081/stream/sse?workflow_id=$WF&types=TEAM_RECRUITED,TEAM_RETIRED"
```

## 快速开始

### 开发测试
```bash
# 启动 Shannon 服务
make dev

# 测试特定工作流的流式传输
make smoke-stream WF_ID=<workflow_id>

# 可选：自定义端点
make smoke-stream WF_ID=workflow-123 ADMIN=http://localhost:8081 GRPC=localhost:50052
```

### 浏览器演示
在浏览器中打开 `docs/streaming-demo.html` 以获得交互式 SSE 演示，包括：
- 可配置的主机、工作流 ID、事件类型过滤器
- 使用 Last-Event-ID 的自动恢复支持
- 实时事件日志显示

## 配置

### 环境变量
- `STREAMING_RING_CAPACITY`（默认：256）- 每个工作流保留用于重放的最近事件数

### 配置文件 (`config/shannon.yaml`)
```yaml
streaming:
  ring_capacity: 256  # 每个工作流保留用于重放的最近事件数
```

配置支持环境变量和 YAML 文件设置，环境变量优先。

## 操作说明

- 重放安全：事件发射是版本门控的，并通过活动路由，保留 Temporal 确定性。
- 背压：对慢速订阅者丢弃事件（非阻塞通道）；客户端应根据需要使用 `last_event_id` 重新连接。
- 安全性：在生产中使用经过身份验证的代理作为管理 HTTP 端口的前端；gRPC 在外部公开时应需要 TLS。

### 反模式和负载考虑
- 避免无界的每客户端缓冲区。进程内管理器使用有界通道和固定环来防止内存增长。
- 不要依赖将每个事件传递给慢速客户端。相反，使用 `last_event_id` 重新连接以确定性地追赶。
- 对于简单的仪表板和日志首选 SSE；仅在需要双向控制消息时使用 WebSocket。
- 对于高扇出，在前面放置外部事件网关（例如 NGINX 或薄 Go 扇出）；进程内管理器不是消息代理。

## 架构

### 事件流
```
工作流 → EmitTaskUpdate（活动） → 流管理器 → 环形缓冲区 + 实时订阅者
                                                  ↓
                        SSE ← HTTP 网关 ← 事件分发 → gRPC 流
                         ↓                        ↓
                    WebSocket ←─────────────── 客户端 SDK
```

### 关键组件
- **流管理器**：具有每个工作流环形缓冲区的内存发布/订阅
- **环形缓冲区**：可配置容量（默认：256 个事件）以支持重放
- **多种协议**：gRPC（企业）、SSE（浏览器原生）、WebSocket（交互式）
- **确定性事件**：所有事件通过 Temporal 活动路由以确保重放安全

### 服务端口
- **管理 HTTP**：8081（SSE `/stream/sse`、WebSocket `/stream/ws`、health、approvals）
- **gRPC**：50052（StreamingService、OrchestratorService、SessionService）

## 集成示例

### Python SDK（伪代码）
```python
import grpc
from shannon.pb import orchestrator_pb2, orchestrator_pb2_grpc

# gRPC 流式传输
channel = grpc.insecure_channel('localhost:50052')
client = orchestrator_pb2_grpc.StreamingServiceStub(channel)
request = orchestrator_pb2.StreamRequest(
    workflow_id='workflow-123',
    types=['AGENT_STARTED', 'AGENT_COMPLETED'],
    last_event_id=0
)

for update in client.StreamTaskExecution(request):
    print(f"代理 {update.agent_id}: {update.type} (seq: {update.seq})")
```

### React 组件
```jsx
import React, { useEffect, useState } from 'react';

function WorkflowStream({ workflowId }) {
  const [events, setEvents] = useState([]);
  
  useEffect(() => {
    const eventSource = new EventSource(
      `/stream/sse?workflow_id=${workflowId}&types=AGENT_COMPLETED`
    );
    
    eventSource.onmessage = (e) => {
      const event = JSON.parse(e.data);
      setEvents(prev => [...prev, event]);
    };
    
    return () => eventSource.close();
  }, [workflowId]);
  
  return (
    <div>
      {events.map(event => (
        <div key={event.seq}>
          {event.type}: {event.agent_id}
        </div>
      ))}
    </div>
  );
}
```

## 故障排除

### 常见问题

**"未收到事件"**
- 验证 workflow_id 存在且正在运行
- 检查工作流中是否启用了 `streaming_v1` 版本门控
- 确保管理 HTTP 端口（8081）可访问

**"重新连接后事件丢失"**
- 使用 `last_event_id` 参数或 `Last-Event-ID` 头部
- 检查环形缓冲区容量 - 比缓冲区大小旧的事件会丢失
- 考虑为较长的工作流增加 `STREAMING_RING_CAPACITY`

**"内存使用率高"**
- 减少配置中的环形缓冲区容量
- 实现客户端过滤以减少事件量
- 对多个并发流使用连接池

### 调试命令
```bash
# 检查流式端点
curl -s http://localhost:8081/health
curl -N "http://localhost:8081/stream/sse?workflow_id=test" | head -10

# 测试 gRPC 连接
grpcurl -plaintext localhost:50052 list shannon.orchestrator.StreamingService

# 监控环形缓冲区使用情况（日志）
docker compose logs orchestrator | grep "streaming"
```

## 路线图

### 阶段 1（当前）
- ✅ 最小事件类型：WORKFLOW_STARTED、AGENT_STARTED、AGENT_COMPLETED、ERROR_OCCURRED
- ✅ 三种协议：gRPC、SSE、WebSocket
- ✅ 使用环形缓冲区支持重放
- ✅ 通过 shannon.yaml 和环境变量配置

### 阶段 2（多代理功能）
- 启用 `roles_v1/supervisor_v1/mailbox_v1` 后的扩展事件类型：
  - `ROLE_ASSIGNED`、`AGENT_MESSAGE_SENT`、`SUPERVISOR_DELEGATED`
  - `POLICY_EVALUATED`、`BUDGET_THRESHOLD`、`WASI_SANDBOX_EVENT`

### 阶段 3（高级功能）
- WebSocket 多路复用以在一个连接中支持多个工作流
- Python/TypeScript SDK 辅助工具以方便使用
- 实时仪表板组件和可视化工具

---

*最后更新：2025 年 1 月*

