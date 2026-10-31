# Dig Sub2API → NewAPI：API 与扣费评估

## 当前范围和结论

用户于 2026-10-31 明确收窄要求：只考虑 Dig Sub2API 接入的 RS、Chat2API，
只需 API 正常使用、按配置价格正常扣费。Cline 和其他上游不在当前范围。
不要求复刻 Sub2API 的套餐、后台和支付流程；异步图片任务路径差异也已明确接受。
此前 `GATEWAY-COMPATIBILITY.md` 中的套餐额度等价问题不作为本范围的否决条件。

**标准 Chat / Responses 调用及钱包扣费具备替换可行性，本机端到端验证通过。**
这不是 Dig 生产上游全模型验收：本次使用合成上游，未访问或修改生产实例。
此前发现的 Cline 包装转换问题与当前范围无关，不作为 RS、Chat2API 的替换阻碍。

## 本机真实 API 与账本结果

使用已构建的候选二进制，新开独立进程和 SQLite 副本，通过应用原生 API
创建测试用户、设置测试余额、创建 Key、配置渠道和价格，再发起真实 HTTP 请求。
未通过 SQL 写入测试余额、渠道或 Key。SQL 只用于一致备份和读取结算证据。
浏览器所用实例、其数据库和会话保持不变。测试进程结束后已停止。
专用 UID 的 IPv4 / IPv6 出站规则只允许回环访问，模型服务仅监听回环地址。

| 场景 | API 结果 | 钱包 / Key / 消费流水 |
|---|---|---|
| Chat JSON、Chat SSE | 正文、工具调用及流结束信号正确 | 每次各扣 1,122 单位，分别仅一条消费流水 |
| Responses JSON、Responses SSE | 正文、工具调用及完成状态正确 | 每次各扣 1,122 单位，分别仅一条消费流水 |
| Responses → Chat 上游，JSON / SSE | 转换后仍返回正文和工具调用 | 每次各扣 1,122 单位，分别仅一条消费流水 |
| 同一用户及 Key，四个并发请求 | 四次全部成功 | 各扣 4,488 单位，恰好四条流水，无丢扣或重复扣款 |
| 图片生成 | 返回可解码的 Base64 PNG 数据及 PNG 文件头 | 按单张配置价格扣 200,000 单位，恰好一条流水 |
| 上游拒绝 | HTTP 400 | 不扣余额、不扣 Key、无消费流水 |
| 无效 Key | HTTP 401 | 不扣余额、不扣 Key、无消费流水 |
| Key 额度为零，钱包余额充足 | HTTP 401，未请求上游 | 不扣款、无消费流水 |
| 钱包为零，Key 已重新启用且额度充足 | HTTP 403，未请求上游 | 不扣款、无消费流水 |

最终执行包括 12 个场景、15 次推理/图片请求，其中 11 次成功调用对应 11 条消费流水。
充值、管理员余额初始化及 Key 配置更新不计入推理请求和消费流水数量。
测试期间 Key 耗尽会改变其状态，因此钱包不足场景先通过原生接口重新启用 Key，
再读回确认有效 Key 和零钱包，避免把 Key 拒绝误认为钱包校验通过。

价格为合成配置，不代表线上销售价格：

```text
1 USD = 100,000,000 单位；default 分组倍率 = 0.2
输入 103 token，其中缓存命中 41；输出 17 token
每百万 token：非缓存输入 0.4 USD，输出 1.6 USD，缓存输入 0.1 USD
((103 - 41) × 0.4 + 17 × 1.6 + 41 × 0.1) / 1,000,000 × 0.2
  = 0.00001122 USD = 1,122 单位
图片配置：0.01 USD/张 × 0.2 = 0.002 USD = 200,000 单位
```

## 先前范围记录：Cline 包装转换，不计入当前验收

这里的实际接入背景是前次评估的 Cline 上游（`api.cline.bot`），不是泛指
RS、Chat2API 或全部 OpenRouter 服务。此次只在本机用对应包装形式重现问题，
未重新调用 Cline；不能据此断言 Cline 当前全部模型都会返回这种结构。

新增 `TestGatewayAssessmentChatEnvelopeCompatibility`，调用实际渠道响应处理器，
用相同模型验证八种响应组合，并检查正文、工具调用、输入/输出 token 和缓存用量。
标准 Chat 通过原生适配器处理；Responses 使用 Advanced Custom 的
`openai_responses_to_openai_chat_completions` 转换器。

- 标准 Chat JSON/SSE、标准 Chat → Responses JSON/SSE 均保留内容和 usage。
- OpenRouter enterprise 模式能解开 Chat JSON 的 `{success:true,data:...}` 包装；
  该类上游在测试中仍输出标准、无包装的 Chat SSE。
- 同一选项设置到 Advanced Custom 后，非流式 Responses 转换仍直接解析外层 JSON：
  **HTTP 200，但 output 为空、正文和工具调用丢失，原始 103/17 usage 被估算输入 11 替代。**
  这项处理器测试未写入账本，不能把估算 usage 本身当作新的实际扣款证据。
- 当前相关配置没有通用响应 JSON 路径提取项；只改上游路径或设置
  `openrouter_enterprise` 不能补齐该转换路径。

这项记录仅保留先前评估证据，不再继续改造或测试 Cline。当前只对标准
RS / Chat2API 协议的合成链路作正面判断，尚不能将它写成真实上游全模型通过。

`messages/count_tokens` 在候选路由源码中处于禁用状态；当前未证明 RS、Chat2API
的实际客户端需要这个端点，不列为替换阻碍。异步图片任务路径不同按用户要求不计作阻碍；其完整
提交、轮询和结果下载生命周期本轮尚未运行，不能以同步图片通过替代证明。

## 版本、复现与边界

- NewAPI 基线 `v1.0.0-rc.47`，提交 `e835b1bfc43b565d625537830dbfb19328925134`，
  加上本分支既有当面付改造。本轮仅增加评估脚本、测试和文档，未更改推理或扣费核心。
- 实际运行候选的源码提交为 `13f0663254f231bc82da40c68158486dba75127f`；
  二进制 SHA-256 为 `26e9458626ed0a16d6e54a50df5bd78dd4e9bd52ebb4288dfbfef5bc13ba98d3`。
  此后的本分支改动为测试和文档；没有把新测试误写成重建后的生产行为。
- Sub2API 只读源码 `c3a9d9db1e63bc8d09df4571a7e118c2977e187d`；其两个现有 Cline
  单元测试本轮通过，证明入口到 Chat 上游的路由行为，不证明当前生产响应或套餐扣账。
- Windows Go `1.27.3`：

```text
go test ./relay/channel/advancedcustom ./relay/channel/openai -count=1 -timeout 120s
go test ./router -run 'TestHostProtocolRegistryDrivesProtocolRoutesOnce|TestSetTaskRouterRegistersWithoutConflict' -count=1 -timeout 90s
```

运行 `tools/gateway-assessment/api_runtime_fixture.py` 必须使用已有的本机隔离 Linux VM，
通过固定回环 SSH 入口和已固定的主机公钥连接，禁止将它指向生产主机。
脚本检查本机候选摘要、复制合成测试库、建立独立 UID 出站限制并在结束时停止进程。
测试账号登录数据只经 stdin 传入；原始数据库、账号、密钥、日志和制品均不随源码上传。
最终结果保留在本机 `api-runtime-result.json` 及独立测试目录中。

相关两个适配器测试包和上述路由测试通过；这些包含已知不兼容的观察断言，
测试命令成功不表示包装转换问题已经修复。端到端扣费验证本轮使用 SQLite，
未声称重新验证 MySQL / PostgreSQL、客户端主动中断、WebSocket、失败重试扣费、
长期负载、异步图片或真实 Dig 上游所有模型。
