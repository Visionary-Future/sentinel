# Sentinel - 智能运维告警调查平台

## 技术架构设计 & MVP 任务拆解

---

## 1. 产品概述

### 1.1 定位

Sentinel 是一个 **AI 驱动的智能运维告警调查平台**，对标 Relvy.ai，面向国内企业运维团队。当告警触发时，Sentinel 自动执行预定义的 Runbook，调查根因，并将结果通过企业微信/钉钉等渠道推送给 on-call 工程师。

### 1.2 核心价值

- **降低 MTTR**: 告警触发后 AI 自动调查，工程师收到的不再是裸告警，而是完整的调查报告
- **标准化应急响应**: 将资深工程师的排查经验沉淀为 Runbook，AI 按步骤执行
- **7x24 无间断**: AI Agent 全天候值守，不受人力轮班限制

### 1.3 差异化

| 维度 | Relvy.ai | Sentinel |
|------|----------|----------|
| 告警来源 | PagerDuty | **Outlook 邮箱 + Slack Channel** + Webhook |
| 云平台 | AWS/GCP 生态 | **阿里云 + 华为云** |
| 通知渠道 | Slack | **企业微信 + 钉钉** |
| 可观测性 | Datadog, New Relic | **阿里云 ARMS/SLS, 华为云 AOM/LTS** |
| 部署模式 | SaaS + 私有化 | 优先私有化部署 |
| LLM | 不透明 | 支持国内外多模型（通义千问、DeepSeek、Claude 等） |

---

## 2. 系统架构

### 2.1 整体架构图

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                            Alert Sources (告警源)                             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐ ┌───────────────┐  │
│  │ Outlook  │ │  Slack   │ │ Webhook  │ │ 阿里云告警    │ │ 华为云告警     │  │
│  │ (Graph   │ │ (Events  │ │ (通用)   │ │ (EventBridge)│ │ (SMN/CES)    │  │
│  │  API)    │ │  API)    │ │          │ │              │ │              │  │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └──────┬───────┘ └──────┬────────┘  │
│       │             │            │               │                │           │
└───────┼─────────────┼────────────┼───────────────┼────────────────┼───────────┘
        │             │            │               │                │
        ▼             ▼            ▼               ▼                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Alert Ingestion Layer (告警接入层)                │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │              Alert Normalizer (告警标准化)                    │    │
│  │  - 解析不同来源的告警格式                                      │    │
│  │  - 统一为内部 AlertEvent 模型                                 │    │
│  │  - 去重 & 聚合                                               │    │
│  └─────────────────────────┬───────────────────────────────────┘    │
└─────────────────────────────┼───────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    Core Engine (核心引擎)                             │
│                                                                     │
│  ┌────────────┐ ┌────────────┐ ┌────────────────┐ ┌──────────────┐ │
│  │  Runbook   │ │  Alert     │ │  Alert         │ │  Software    │ │
│  │  Registry  │ │  Router    │ │  Correlation   │ │  Catalog     │ │
│  │ (Runbook库)│ │ (告警路由)  │ │  Engine        │ │ (服务目录)    │ │
│  │            │ │            │ │ (历史关联引擎)  │ │              │ │
│  └─────┬──────┘ └─────┬──────┘ └───────┬────────┘ └──────┬───────┘ │
│        │              │                │                  │         │
│        ▼              ▼                ▼                  ▼         │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │              Investigation Engine (调查引擎)                  │    │
│  │                                                              │    │
│  │  ┌─────────────┐  ┌──────────────┐  ┌───────────────────┐   │    │
│  │  │ AI Agent    │  │ Step         │  │ Tool Executor     │   │    │
│  │  │ (LLM 编排)  │  │ Orchestrator │  │ (工具执行器)       │   │    │
│  │  │             │  │ (步骤编排)    │  │                   │   │    │
│  │  └─────────────┘  └──────────────┘  └───────────────────┘   │    │
│  │                                                              │    │
│  └──────────────────────────┬──────────────────────────────────┘    │
│                              │                                      │
└──────────────────────────────┼──────────────────────────────────────┘
                               │
                ┌──────────────┼──────────────┐
                ▼              ▼              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                  Data Source Layer (数据源层)                         │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────┐ │
│  │ 阿里云 SLS  │  │ 华为云 LTS  │  │ 阿里云 ARMS │  │ 华为云AOM │ │
│  │ (日志)      │  │ (日志)      │  │ (APM/Trace) │  │ (监控)    │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └───────────┘ │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────┐ │
│  │ Elasticsearch│ │ 阿里云 云监控│ │ 华为云 CES  │  │  GitHub   │ │
│  │             │  │ (Metrics)   │  │ (Metrics)   │  │ (代码)    │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └───────────┘ │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                  Output Layer (输出层)                                │
│                                                                     │
│  ┌──────────────────┐  ┌───────────────────────────────────────┐   │
│  │ Notification     │  │ Investigation Notebook (调查笔记本)    │   │
│  │ ┌──────┐┌──────┐ │  │ - Web UI 展示调查过程                  │   │
│  │ │企业微信││钉钉  │ │  │ - 可交互：编辑查询、追问、分享         │   │
│  │ └──────┘└──────┘ │  │ - 完整审计轨迹                         │   │
│  │ ┌──────┐┌──────┐ │  │                                       │   │
│  │ │Slack ││Outlk │ │  │                                       │   │
│  │ └──────┘└──────┘ │  │                                       │   │
│  │ ┌──────┐         │  │                                       │   │
│  │ │Webhk │         │  │                                       │   │
│  │ └──────┘└──────┘ │  │                                       │   │
│  └──────────────────┘  └───────────────────────────────────────┘   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.2 模块详细设计

#### 2.2.1 Alert Ingestion Layer (告警接入层)

负责从多种来源接收告警，统一为内部格式。

**内部告警模型 (AlertEvent)**:

```typescript
interface AlertEvent {
  id: string;                    // 唯一标识
  source: AlertSource;           // 来源: outlook | slack | webhook | aliyun | huaweicloud
  severity: Severity;            // 严重程度: critical | warning | info
  title: string;                 // 告警标题
  description: string;           // 告警描述
  service: string;               // 关联服务名
  labels: Record<string, string>;// 标签（环境、区域等）
  rawPayload: unknown;           // 原始数据
  receivedAt: string;            // 接收时间 (ISO 8601)
  fingerprint: string;           // 去重指纹
  embedding: number[];           // 文本向量 (用于相似告警检索)
  correlationId: string | null;  // 关联组 ID (同一根因的告警归为一组)
}
```

**邮件 (IMAP) 接入**:
- 通过 IMAP 协议轮询指定邮箱，支持所有标准 IMAP 服务商
- 适配国内主流邮箱：163企业邮箱、QQ企业邮箱、阿里企业邮箱
- 支持 Outlook/Microsoft 365（国际版及21Vianet版）、Gmail 等
- 轮询未读邮件 → 匹配发件人/主题/正文关键词过滤器 → 处理后标记已读
- 严重程度从主题/正文关键词自动推断（critical / warning / info）

**Slack Channel 接入**:
- 使用 Slack Events API (Socket Mode 或 HTTP) 监听指定 Channel 的新消息
- 通过 Slack App (Bot Token) 订阅 `message.channels` / `message.groups` 事件
- 支持配置监听规则：指定 Channel、发送者 (Bot)、消息关键词过滤
- 解析 Slack 消息内容（纯文本 + Block Kit 结构化数据）提取告警字段
- 支持 Thread 关联：同一告警的后续消息自动关联到同一 AlertEvent
- 典型场景：Prometheus AlertManager → Slack Channel → Sentinel 自动拾取

**阿里云告警接入**:
- EventBridge 事件总线 → HTTP Webhook 推送
- 支持云监控 (CloudMonitor) 告警规则直接 Webhook 回调

**华为云告警接入**:
- SMN (Simple Message Notification) → HTTP(S) Webhook
- CES (Cloud Eye Service) 告警规则回调

**通用 Webhook 接入**:
- 提供标准 REST 端点，接受 JSON payload
- 支持自定义字段映射模板

#### 2.2.2 Core Engine (核心引擎)

##### Runbook Registry (Runbook 注册中心)

Runbook 使用 Markdown 格式编写，存储在 Git 仓库或数据库中。

```markdown
# API Latency Runbook

## Trigger
- alert.title contains "P99 latency"
- alert.severity in [critical, warning]
- alert.service matches "order-*"

## Steps
- 查询 $service 过去 30 分钟的 P99 延迟趋势
- 检查哪些 API 端点延迟最高
- 检查是否有关联的吞吐量变化
- 查询最近的部署记录
- 如果有最近部署，review 代码变更
- 检查下游依赖服务的健康状态
- 如果是流量激增导致，考虑扩容建议

## Escalation
- team: platform-team
- channel: wecom://oncall-group
```

##### Alert Correlation Engine (告警关联引擎)

新告警进入时，自动与历史告警进行关联分析，为 AI Agent 提供历史上下文。

**关联分析流程**:

```
新告警进入
    │
    ▼
┌──────────────────────────────────────────────────────────────┐
│                 Alert Correlation Engine                      │
│                                                              │
│  ┌────────────────┐  ┌────────────────┐  ┌───────────────┐  │
│  │ 1. 指纹匹配    │  │ 2. 语义相似度   │  │ 3. 时空关联   │  │
│  │ (精确匹配)     │  │ (向量检索)      │  │ (同服务/时窗) │  │
│  └───────┬────────┘  └───────┬────────┘  └──────┬────────┘  │
│          │                   │                   │           │
│          ▼                   ▼                   ▼           │
│  ┌─────────────────────────────────────────────────────┐     │
│  │              关联结果聚合 & 排序                       │     │
│  └──────────────────────┬──────────────────────────────┘     │
│                          │                                   │
│  ┌──────────────────────▼──────────────────────────────┐     │
│  │              历史上下文构建                            │     │
│  │  - 相似告警摘要 + 历史调查结论                         │     │
│  │  - 告警频率/趋势分析                                  │     │
│  │  - 历史修复方案                                       │     │
│  └─────────────────────────────────────────────────────┘     │
│                                                              │
└──────────────────────────────────────────────────────────────┘
    │
    ▼
注入 AI Agent 上下文 → 开始调查
```

**三层关联策略**:

| 层次 | 方法 | 说明 |
|------|------|------|
| **指纹精确匹配** | fingerprint 相同 | 同一告警规则反复触发（如同一监控项连续报警），直接关联为同一告警组 |
| **语义相似度** | 向量检索 (Embedding) | 标题/描述文本相似的告警（如 "order-service OOM" 和 "order-service 内存使用率 95%"），基于余弦相似度 Top-K 检索 |
| **时空关联** | 服务 + 时间窗口 | 同一服务或有依赖关系的服务在时间窗口内（如 ±15 分钟）出现的告警，可能是同一根因引起的级联故障 |

**历史上下文数据结构**:

```typescript
interface AlertHistoryContext {
  // 相似告警
  similarAlerts: {
    alert: AlertEvent;
    similarity: number;           // 相似度 0-1
    investigation: {
      rootCause: string;          // 历史调查的根因结论
      resolution: string;         // 修复方案
      resolvedAt: string;         // 解决时间
      mttr: number;               // 修复耗时 (秒)
    } | null;
  }[];

  // 告警统计
  stats: {
    occurrenceCount: number;      // 同指纹告警在过去 N 天内出现次数
    lastOccurrence: string;       // 上次出现时间
    avgMttr: number;              // 平均修复时间
    isFlapping: boolean;          // 是否在抖动 (频繁触发/恢复)
    trend: 'increasing' | 'stable' | 'decreasing'; // 频率趋势
  };

  // 关联告警组 (同一时间窗口内的相关告警)
  correlatedAlerts: {
    alert: AlertEvent;
    relation: 'same_service' | 'dependency' | 'same_fingerprint';
  }[];
}
```

**向量检索方案**:

- 告警入库时，用 LLM Embedding API 将 `title + description + labels` 生成向量
- 存储在 PostgreSQL (pgvector 扩展) 中，支持高效相似度检索
- 新告警进入时，检索最近 90 天内相似度 > 0.8 的历史告警
- 优先返回有成功调查结论的历史告警（已解决 > 未解决）

**注入 Agent 调查流程**:

AI Agent 在开始 Runbook 步骤之前，会先收到历史上下文摘要：

```
系统提示:
"以下是与当前告警相关的历史信息：

## 相似告警历史
1. [2025-03-12] 同一告警出现过 → 根因: order-service v2.4.2 引入的 N+1 查询
   修复方案: 回滚到 v2.4.1，后续在 v2.4.3 修复
   MTTR: 23 分钟

2. [2025-02-28] 类似延迟告警 → 根因: 数据库连接池耗尽
   修复方案: 增大连接池 max_connections 从 20 到 50
   MTTR: 45 分钟

## 告警统计
- 此告警过去 30 天出现 7 次，频率呈上升趋势
- 平均 MTTR: 34 分钟
- 非抖动告警

## 当前关联告警
- [同时] payment-service 也出现 5xx 错误率上升 (下游依赖)

请结合以上历史信息执行 Runbook 调查。优先验证历史根因是否再次出现。"
```

##### Alert Router (告警路由)

根据告警的标签、服务名、严重程度，匹配对应的 Runbook 并触发调查：

```
AlertEvent → 告警关联分析 → 匹配 Runbook Trigger 条件 → 创建 Investigation (含历史上下文) → 执行 Steps
```

##### Software Catalog (服务目录)

维护服务、团队、依赖关系的知识图谱：

```typescript
interface Service {
  name: string;
  team: string;
  repository: string;           // GitHub 仓库地址
  dependencies: string[];       // 依赖的服务列表
  alertSources: AlertSource[];  // 关联的告警源
  dataSources: DataSource[];    // 关联的数据源配置
  oncallChannel: string;        // on-call 通知渠道
}
```

##### Investigation Engine (调查引擎)

核心 AI Agent 模块，负责按 Runbook 步骤执行调查：

```
┌─────────────────────────────────────────────┐
│           Investigation Engine              │
│                                             │
│  ┌─────────┐    ┌─────────────────────┐     │
│  │  LLM    │◄──►│  Step Orchestrator  │     │
│  │ (通义/   │    │  - 解析 Runbook     │     │
│  │  Claude/ │    │  - 生成查询计划     │     │
│  │  DeepSk) │    │  - 分析结果        │     │
│  └─────────┘    │  - 决定下一步      │     │
│                  └──────────┬──────────┘     │
│                             │               │
│                  ┌──────────▼──────────┐     │
│                  │  Tool Executor      │     │
│                  │  ┌───────────────┐  │     │
│                  │  │ query_logs    │  │     │
│                  │  │ query_metrics │  │     │
│                  │  │ query_traces  │  │     │
│                  │  │ search_history│  │     │
│                  │  │ search_code   │  │     │
│                  │  │ check_deploy  │  │     │
│                  │  │ analyze_deps  │  │     │
│                  │  │ run_action    │  │     │
│                  │  └───────────────┘  │     │
│                  └────────────────────┘     │
└─────────────────────────────────────────────┘
```

**Agent 工作流**:

1. 接收 AlertEvent + 匹配的 Runbook
2. **注入历史上下文**: 从 Alert Correlation Engine 获取相似告警、历史调查结论、告警统计
3. LLM 解析 Runbook 步骤，结合 **告警上下文 + 历史上下文** 生成具体查询计划
4. 逐步执行：调用 Tool → 获取结果 → LLM 分析 → 决定下一步
   - Agent 可随时调用 `search_history` 工具主动检索更多历史告警
5. 生成调查报告（含 root cause 分析、建议操作、**与历史告警的对比结论**）
6. 危险操作（如扩缩容）需 Human-in-the-loop 审批
7. 输出结果到通知渠道 + Investigation Notebook
8. **回写历史**: 调查完成后，将根因和修复方案写回告警记录，供未来检索

**LLM 适配层**:

```typescript
interface LLMProvider {
  chat(messages: Message[], tools: Tool[]): Promise<Response>;
}

// 支持的 Provider
// - TongyiProvider   (通义千问 / 阿里云百炼)
// - DeepSeekProvider (DeepSeek)
// - ClaudeProvider   (Anthropic Claude)
// - OpenAIProvider   (OpenAI GPT)
```

#### 2.2.3 Data Source Layer (数据源层)

每个数据源实现统一的 DataSource 接口：

```typescript
interface DataSource {
  type: 'logs' | 'metrics' | 'traces' | 'events' | 'code';
  queryLogs(params: LogQuery): Promise<LogResult>;
  queryMetrics(params: MetricQuery): Promise<MetricResult>;
  queryTraces?(params: TraceQuery): Promise<TraceResult>;
}
```

**阿里云数据源**:

| 服务 | 用途 | SDK/API |
|------|------|---------|
| SLS (日志服务) | 日志查询与分析 | aliyun-log SDK, SQL 查询语法 |
| ARMS | APM / 分布式追踪 | OpenAPI |
| 云监控 (CloudMonitor) | 指标查询 | DescribeMetricList API |
| ACK (容器服务) | K8s 资源状态 | Kubernetes API |

**华为云数据源**:

| 服务 | 用途 | SDK/API |
|------|------|---------|
| LTS (日志服务) | 日志查询与分析 | LTS SDK |
| AOM (应用运维管理) | 监控指标/告警 | AOM API |
| CES (云监控) | 基础设施指标 | CES SDK |
| CCE (容器引擎) | K8s 资源状态 | Kubernetes API |

**通用数据源**:

| 服务 | 用途 |
|------|------|
| Elasticsearch | 日志查询 |
| GitHub | 代码搜索、部署记录、代码变更 |

#### 2.2.4 Output Layer (输出层)

##### Notification (通知)

```typescript
interface NotificationChannel {
  send(investigation: InvestigationResult): Promise<void>;
}
```

**Slack**:
- 通过 Slack Bot 将调查结果发送到指定 Channel 或 Thread
- 支持 Block Kit 富文本格式展示调查摘要
- 支持 Thread 回复：在原始告警消息的 Thread 中回复调查结果
- 支持交互式消息：审批按钮（通过 Slack Interactivity）

**企业微信 (WeCom)**:
- 通过企业微信群机器人 Webhook 推送
- 支持 Markdown 格式的调查摘要卡片
- 支持交互式消息：审批按钮（扩缩容等危险操作）
- 可选：企业微信应用消息（需注册企业应用）

**钉钉 (DingTalk)**:
- 通过钉钉群自定义机器人 Webhook 推送
- 支持 ActionCard 消息格式展示调查结果
- 支持交互式卡片：审批按钮
- 可选：钉钉工作通知（需创建应用）

**Outlook 邮件回复**:
- 通过 Microsoft Graph API 回复原始告警邮件
- 调查报告以 HTML 格式嵌入邮件正文

**通用 Webhook**:
- 标准 JSON payload 推送到自定义端点

##### Investigation Notebook (调查笔记本)

Web UI 展示每次调查的完整过程：

- 按步骤展示：查询 → 结果 → AI 分析 → 结论
- 数据可视化：时序图、表格、拓扑图
- 交互功能：编辑查询重新执行、追问 AI、导出分享
- 完整审计轨迹

---

## 3. 技术选型

### 3.1 技术栈

| 层次 | 技术 | 理由 |
|------|------|------|
| **后端语言** | Go | 高并发、低资源消耗、适合云原生场景 |
| **Web 框架** | Gin / Echo | 轻量高性能 |
| **AI Agent 编排** | 自研 (Go) | 灵活控制 Agent 工作流，避免框架锁定 |
| **数据库** | PostgreSQL + pgvector | 结构化数据 + 告警向量存储，支持相似度检索 |
| **消息队列** | Redis Streams | 告警事件队列、任务调度，轻量易部署 |
| **前端** | React + TypeScript | Investigation Notebook UI |
| **前端框架** | Next.js | SSR + API Routes |
| **图表库** | Recharts / ECharts | 时序数据可视化 |
| **容器化** | Docker + Docker Compose | 本地开发与私有化部署 |
| **编排** | Kubernetes (Helm) | 生产环境部署 |

### 3.2 项目结构

```
sentinel/
├── cmd/
│   └── sentinel/           # 主程序入口
├── internal/
│   ├── alert/              # 告警接入层
│   │   ├── source/         # 各告警源适配器
│   │   │   ├── outlook.go
│   │   │   ├── slack.go
│   │   │   ├── webhook.go
│   │   │   ├── aliyun.go
│   │   │   └── huaweicloud.go
│   │   ├── normalizer.go   # 告警标准化
│   │   ├── dedup.go        # 去重
│   │   └── correlation.go  # 历史告警关联引擎
│   ├── runbook/            # Runbook 管理
│   │   ├── parser.go       # Markdown 解析
│   │   ├── registry.go     # 注册中心
│   │   └── matcher.go      # 告警-Runbook 匹配
│   ├── investigation/      # 调查引擎
│   │   ├── engine.go       # 调查编排
│   │   ├── agent.go        # AI Agent
│   │   ├── step.go         # 步骤执行
│   │   └── tool/           # 工具集
│   │       ├── logs.go
│   │       ├── metrics.go
│   │       ├── traces.go
│   │       ├── history.go  # 历史告警检索
│   │       ├── code.go
│   │       └── action.go
│   ├── datasource/         # 数据源适配层
│   │   ├── datasource.go   # 统一接口
│   │   ├── aliyun/
│   │   │   ├── sls.go      # 阿里云日志服务
│   │   │   ├── arms.go     # ARMS
│   │   │   └── cms.go      # 云监控
│   │   ├── huaweicloud/
│   │   │   ├── lts.go      # 华为云日志
│   │   │   ├── aom.go      # AOM
│   │   │   └── ces.go      # CES
│   │   ├── elasticsearch/
│   │   └── github/
│   ├── catalog/            # 服务目录
│   │   ├── service.go
│   │   ├── team.go
│   │   └── dependency.go
│   ├── notify/             # 通知渠道
│   │   ├── notifier.go     # 统一接口
│   │   ├── slack.go        # Slack
│   │   ├── wecom.go        # 企业微信
│   │   ├── dingtalk.go     # 钉钉
│   │   ├── outlook.go      # Outlook 邮件
│   │   └── webhook.go      # 通用 Webhook
│   ├── llm/                # LLM 适配层
│   │   ├── provider.go     # 统一接口
│   │   ├── tongyi.go       # 通义千问
│   │   ├── deepseek.go     # DeepSeek
│   │   ├── claude.go       # Claude
│   │   └── openai.go       # OpenAI
│   └── api/                # HTTP API
│       ├── handler/
│       └── middleware/
├── web/                    # 前端 (Next.js)
│   ├── src/
│   │   ├── app/
│   │   ├── components/
│   │   │   ├── notebook/   # Investigation Notebook
│   │   │   ├── catalog/    # 服务目录
│   │   │   └── runbook/    # Runbook 编辑器
│   │   └── lib/
│   └── package.json
├── deploy/
│   ├── docker-compose.yml
│   └── helm/
├── configs/
│   └── sentinel.yaml       # 主配置文件
├── docs/
└── go.mod
```

---

## 4. MVP 任务拆解

### Phase 0: 项目基础 (Week 1)

> 目标: 搭建项目骨架，跑通最基本的端到端流程

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 0.1 | 初始化 Go 项目结构 | go.mod, cmd/, internal/ 骨架 | 0.5d |
| 0.2 | 搭建 PostgreSQL schema | 告警、调查、Runbook 表结构 + pgvector 扩展 | 0.5d |
| 0.3 | Docker Compose 开发环境 | PostgreSQL (pgvector) + Redis + 应用一键启动 | 0.5d |
| 0.4 | 基础 HTTP API 框架 | 健康检查、路由注册、中间件 | 0.5d |
| 0.5 | CI 流水线 | lint + test + build | 0.5d |

### Phase 1: 告警接入 (Week 2-3)

> 目标: 能从 Outlook 邮箱和 Slack Channel 接收告警并标准化

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 1.1 | AlertEvent 数据模型 & 存储 | model + repository + migration | 0.5d |
| 1.2 | 通用 Webhook 告警接入 | POST /api/v1/alerts 端点 | 0.5d |
| 1.3 | Outlook 邮箱接入 (Graph API) | 邮箱轮询/订阅 → AlertEvent | 1.5d |
| 1.4 | 邮件内容解析器 | 正则 + LLM 提取告警字段 | 1d |
| 1.5 | Slack Channel 接入 (Events API) | Slack App + Socket Mode 监听消息 | 1.5d |
| 1.6 | Slack 消息解析器 | 解析纯文本/Block Kit，提取告警字段 + Thread 关联 | 1d |
| 1.7 | 告警去重 & 指纹计算 | fingerprint 生成 + 去重逻辑 | 0.5d |

### Phase 1.5: 历史告警关联 (Week 3)

> 目标: 新告警能自动关联历史告警，为 AI 调查提供历史上下文

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 1.8 | 告警 Embedding 生成 | 入库时调用 Embedding API 生成向量，存入 pgvector | 1d |
| 1.9 | 指纹匹配 + 相似度检索 | fingerprint 精确匹配 + 向量 Top-K 相似检索 | 1d |
| 1.10 | 时空关联分析 | 同服务/依赖服务 ± 时间窗口内的告警关联 | 1d |
| 1.11 | AlertHistoryContext 构建 | 聚合历史调查结论、告警统计、关联告警组 | 1d |
| 1.12 | search_history Agent Tool | Agent 可主动检索历史告警的工具 | 0.5d |
| 1.13 | 调查结论回写 | 调查完成后将根因/修复方案写回告警记录 | 0.5d |

### Phase 2: Runbook 引擎 (Week 4)

> 目标: 能解析 Markdown Runbook，匹配告警并编排步骤

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 2.1 | Runbook Markdown 解析器 | 解析 trigger/steps/escalation | 1d |
| 2.2 | Runbook CRUD API | 创建/读取/更新/删除 Runbook | 0.5d |
| 2.3 | Alert → Runbook 匹配引擎 | 基于标签/服务名/关键词匹配 | 1d |
| 2.4 | Investigation 数据模型 | 调查记录 model + 状态机 | 0.5d |
| 2.5 | 步骤编排器骨架 | 按顺序执行步骤，记录结果 | 1d |

### Phase 3: AI Agent & LLM 集成 (Week 5-6)

> 目标: AI Agent 能理解 Runbook 步骤，生成查询，分析结果

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 3.1 | LLM Provider 统一接口 | provider.go + tool calling 协议 | 1d |
| 3.2 | 通义千问 Provider | 阿里云百炼 API 适配 | 1d |
| 3.3 | Claude/OpenAI Provider | Anthropic + OpenAI API 适配 | 0.5d |
| 3.4 | Agent 核心循环 | 接收步骤 → LLM 规划 → 调用工具 → 分析结果 → 下一步 | 2d |
| 3.5 | Tool 注册框架 | Tool 定义 + JSON Schema + 执行器 | 1d |
| 3.6 | 调查报告生成 | LLM 总结调查过程，输出结构化报告 | 1d |

### Phase 4: 数据源集成 (Week 6-7)

> 目标: 能查询阿里云/华为云的日志和指标

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 4.1 | DataSource 统一接口 | 日志/指标/追踪查询抽象 | 0.5d |
| 4.2 | 阿里云 SLS 日志查询 | SLS SDK 集成，SQL 查询 | 1.5d |
| 4.3 | 阿里云云监控指标查询 | CloudMonitor API 集成 | 1d |
| 4.4 | 华为云 LTS 日志查询 | LTS SDK 集成 | 1.5d |
| 4.5 | 华为云 CES 指标查询 | CES SDK 集成 | 1d |
| 4.6 | Elasticsearch 日志查询 | ES client 集成 | 1d |
| 4.7 | query_logs / query_metrics Tool | 连接 Agent 与 DataSource | 1d |

### Phase 5: 通知渠道 (Week 8)

> 目标: 调查结果能推送到企业微信和钉钉

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 5.1 | Notifier 统一接口 | channel 抽象 + 消息模板 | 0.5d |
| 5.2 | Slack 通知 (复用 Bot) | Block Kit 卡片 + Thread 回复 | 1d |
| 5.3 | 企业微信群机器人通知 | Webhook + Markdown 卡片 | 1d |
| 5.4 | 钉钉群机器人通知 | Webhook + ActionCard | 1d |
| 5.5 | Outlook 邮件回复 | Graph API 发送回复邮件 | 1d |
| 5.6 | 通知模板引擎 | Go template 渲染调查摘要 | 0.5d |
| 5.7 | 通知路由配置 | 按服务/团队/严重程度路由到不同渠道 | 0.5d |

### Phase 6: Investigation Notebook UI (Week 9-10)

> 目标: Web 界面展示调查过程

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 6.1 | Next.js 项目初始化 | 项目骨架 + 路由 + 布局 | 0.5d |
| 6.2 | 调查列表页 | 展示所有调查记录，筛选/搜索 | 1d |
| 6.3 | 调查详情 - 步骤时间线 | 按步骤展示调查过程 | 1.5d |
| 6.4 | 数据可视化组件 | 时序图、表格、JSON 展示 | 1.5d |
| 6.5 | 告警历史面板 | 相似告警列表、频率趋势图、历史根因速览 | 1d |
| 6.6 | Runbook 管理页 | Runbook 列表 + Markdown 编辑器 | 1d |
| 6.7 | 服务目录页 | 服务列表 + 依赖关系图 | 1d |
| 6.8 | 实时调查流 (SSE/WebSocket) | 调查进行中实时刷新步骤 | 1d |

### Phase 7: 端到端联调 & 打磨 (Week 11)

> 目标: 完整跑通一个真实告警场景

| # | 任务 | 产出 | 预估 |
|---|------|------|------|
| 7.1 | 端到端集成测试 | 告警 → 调查 → 通知 完整流程 | 1.5d |
| 7.2 | 示例 Runbook 编写 | 3-5 个常见场景的 Runbook 示例 | 1d |
| 7.3 | 配置管理 & 文档 | 部署文档、配置说明、快速上手指南 | 1d |
| 7.4 | 错误处理 & 重试机制 | 各层的异常处理和容错 | 1d |
| 7.5 | Helm Chart | Kubernetes 部署包 | 1d |

---

## 5. MVP 范围定义

### 包含 (In Scope)

- 告警接入: Outlook 邮箱 + Slack Channel + 通用 Webhook
- 数据源: 阿里云 SLS + 阿里云云监控 (先做一个云的，另一个 Phase 2)
- AI Agent: 通义千问 (主) + Claude (备)
- 通知: Slack + 企业微信 + 钉钉
- Runbook: Markdown 格式，CRUD 管理
- UI: 基础 Investigation Notebook
- 部署: Docker Compose (开发) + Helm (生产)

### 不包含 (Out of Scope for MVP)

- 华为云数据源集成 (Phase 2 优先级)
- 阿里云 ARMS Trace 分析 (Phase 2)
- Human-in-the-loop 审批流 (Phase 2)
- 多租户 (Phase 3)
- RBAC 权限管理 (Phase 3)
- 自动修复 Action (Phase 3)
- 知识图谱 / 服务拓扑自动发现 (Phase 3)

### MVP 成功标准

1. 能从 Outlook 邮箱和 Slack Channel 自动接收告警
2. 能匹配 Runbook 并自动触发 AI 调查
3. AI 能查询阿里云 SLS 日志和云监控指标
4. 调查结果能推送到企业微信/钉钉群
5. 能在 Web UI 查看调查过程和报告

---

## 6. 关键设计决策

### 6.1 为什么选 Go 而不是 Python？

- 编译为单二进制，私有化部署简单
- 并发模型天然适合处理多个告警的并行调查
- 阿里云/华为云 SDK 均有官方 Go 版本
- 资源占用低，适合在客户环境运行

### 6.2 Agent 编排为什么自研？

- LangChain/LlamaIndex 等框架 Go 生态不成熟
- 我们的 Agent 模式相对固定（Runbook 步骤驱动），不需要通用框架
- 自研可以精细控制 token 使用、超时、重试等行为
- 避免框架升级带来的 breaking change

### 6.3 为什么先做阿里云、后做华为云？

- 阿里云市场份额更大，覆盖更多潜在客户
- SLS 的查询语法更成熟，SDK 文档更完善
- 架构上预留了 DataSource 接口，后续接入华为云只需实现接口

### 6.4 LLM 多模型策略

- **日常调查**: 通义千问 qwen-max（成本低、国内延迟低、无数据出境）
- **复杂分析**: Claude Sonnet（推理能力强、tool calling 稳定）
- **支持切换**: 配置文件指定默认模型，可按 Runbook 覆盖
- **私有化场景**: 支持对接客户自有模型端点 (OpenAI 兼容 API)

---

## 7. 风险与应对

| 风险 | 影响 | 应对 |
|------|------|------|
| Outlook Graph API 权限审批慢 | 告警接入延迟 | 先用邮件轮询模式，Webhook 作为优化 |
| Slack API rate limit (1 msg/s per channel) | 高频告警漏接 | 消息批量拉取 + 本地队列缓冲 |
| 阿里云/华为云 API 限流 | 查询失败 | 实现退避重试 + 查询结果缓存 |
| LLM 幻觉导致错误分析 | 用户信任度下降 | 每步展示原始数据 + AI 分析，用户可验证 |
| Runbook 编写门槛高 | 用户上手慢 | 提供丰富示例模板 + AI 辅助生成 Runbook |
| Token 消耗过高 | 成本不可控 | 设置单次调查 token 上限，优化 prompt |

---

## 附录 A: 配置文件示例

```yaml
# configs/sentinel.yaml

server:
  port: 8080

database:
  host: localhost
  port: 5432
  name: sentinel
  user: sentinel
  password: ${DB_PASSWORD}

redis:
  addr: localhost:6379

# 告警源配置
alert_sources:
  outlook:
    enabled: true
    imap_host: imap.163.com          # 163企业邮箱; QQ企业邮箱: imap.exmail.qq.com
    imap_port: 993
    username: alerts@company.com
    password: ${EMAIL_AUTH_CODE}     # 授权码，非登录密码
    folder: INBOX
    tls: true
    poll_interval: 30s
    filters:
      subjects: ["告警", "Alert", "FIRING"]
      senders: ["noreply@aliyun.com"]

  slack:
    enabled: true
    bot_token: ${SLACK_BOT_TOKEN}
    app_token: ${SLACK_APP_TOKEN}       # Socket Mode 需要
    signing_secret: ${SLACK_SIGNING_SECRET}
    channels:
      - id: C01ABCDEF01                 # Channel ID
        name: "#ops-alerts"
        filters:
          bot_users: ["U_ALERTMANAGER"] # 只监听特定 Bot 的消息
          keywords: ["FIRING", "alert", "告警"]
      - id: C02ABCDEF02
        name: "#infra-alerts"

  webhook:
    enabled: true
    path: /api/v1/alerts/webhook
    secret: ${WEBHOOK_SECRET}

# 数据源配置
data_sources:
  aliyun_sls:
    enabled: true
    access_key_id: ${ALIYUN_AK_ID}
    access_key_secret: ${ALIYUN_AK_SECRET}
    endpoint: cn-hangzhou.log.aliyuncs.com
    project: my-project

  aliyun_cms:
    enabled: true
    access_key_id: ${ALIYUN_AK_ID}
    access_key_secret: ${ALIYUN_AK_SECRET}
    region: cn-hangzhou

# LLM 配置
llm:
  default_provider: tongyi
  providers:
    tongyi:
      api_key: ${TONGYI_API_KEY}
      model: qwen-max
    claude:
      api_key: ${CLAUDE_API_KEY}
      model: claude-sonnet-4-20250514

# 通知渠道配置
notification:
  slack:
    enabled: true
    # 复用 alert_sources.slack 的 Bot Token
    default_channel: C01ABCDEF01
    reply_in_thread: true          # 在告警消息 Thread 中回复调查结果

  wecom:
    enabled: true
    webhook_url: ${WECOM_WEBHOOK_URL}

  dingtalk:
    enabled: true
    webhook_url: ${DINGTALK_WEBHOOK_URL}
    secret: ${DINGTALK_SECRET}

  outlook:
    enabled: true
    # 复用 alert_sources.outlook 的认证配置
    reply_to_alert: true
```

## 附录 B: API 端点设计

```
# 告警
POST   /api/v1/alerts/webhook          # 接收 Webhook 告警
GET    /api/v1/alerts                   # 告警列表
GET    /api/v1/alerts/:id               # 告警详情

# 告警历史关联
GET    /api/v1/alerts/:id/similar       # 相似历史告警
GET    /api/v1/alerts/:id/correlated    # 时空关联告警组
GET    /api/v1/alerts/:id/stats         # 告警统计 (频率/趋势/MTTR)

# Runbook
GET    /api/v1/runbooks                 # Runbook 列表
POST   /api/v1/runbooks                 # 创建 Runbook
GET    /api/v1/runbooks/:id             # Runbook 详情
PUT    /api/v1/runbooks/:id             # 更新 Runbook
DELETE /api/v1/runbooks/:id             # 删除 Runbook

# 调查
GET    /api/v1/investigations           # 调查列表
GET    /api/v1/investigations/:id       # 调查详情 (含步骤)
POST   /api/v1/investigations/:id/retry # 重新执行调查
GET    /api/v1/investigations/:id/stream # SSE 实时步骤流

# 服务目录
GET    /api/v1/catalog/services         # 服务列表
POST   /api/v1/catalog/services         # 创建服务
GET    /api/v1/catalog/services/:id     # 服务详情
PUT    /api/v1/catalog/services/:id     # 更新服务

# 数据源
GET    /api/v1/datasources              # 数据源列表
POST   /api/v1/datasources/test         # 测试数据源连接

# 配置
GET    /api/v1/config/notifications     # 通知渠道配置
PUT    /api/v1/config/notifications     # 更新通知配置
```
