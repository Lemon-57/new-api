# LAC-03 isolated API acceptance

Scope: deploy an independent NewAPI candidate on LAC-03 and verify real RS and
Chat2API inference with correct wallet, API key and usage-log settlement.
Do not switch customer traffic, change production account state or proxies,
or migrate existing balances and databases.

The application supports `BIND_ADDRESS`, defaulting to its existing wildcard
listen address. Set `BIND_ADDRESS=127.0.0.1` for the acceptance service. Access it
through an authenticated SSH local forward. No public reverse-proxy or DNS
change is required. Build the executable locally and verify the SHA-256 both
before transfer and on the destination.

Use a separate service, state directory, SQLite database, administrator,
customer key and synthetic wallet credit. Disable new checkout methods and
background channel updates. Apply a modest resource budget to this candidate;
the existing RS and Chat2API resource policies and serving instances stay intact.
Real inference must use the existing authenticated upstream endpoints and model
mapping. Read source configuration without copying refresh/access tokens or
account passwords. Never include upstream API secrets in reports or Git.

Before and after deployment, record physical RAM and swap separately, pressure,
disk headroom, application/dependency health, active traffic and serving process
identities. Rollback consists of stopping only the acceptance service after its
own requests drain; it does not touch customer routing or production services.
Preserve its database and small verification manifest.

Predeployment route tests exposed previously undeclared Alipay dashboard routes.
Declare those as session-only, preserving the existing dashboard credential
requirement and rejecting all personal access tokens on these operations. The
signed public payment notification remains outside dashboard authentication.
This does not enable payment or expand payment acceptance scope.

The narrow authorization correction follows default-deny, least privilege and
server-side per-request checks from the [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html),
with the [Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
and [Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
guidance reviewed against the published stable [ASVS 5.0.0](https://asvs.owasp.org/).
Existing authorization tests cover route completeness and token enforcement;
the focused regression asserts all six Alipay dashboard routes remain session-only.
This is not a claim of complete ASVS certification.

## 实际验收结果（2026-10-31）

**已在 LAC-03 独立部署，真实 API 与扣费验收通过。** 实测仅使用 Dig 现有的
RS 和 Chat2API；没有切换客户入口、迁移客户数据库或变更上游账号配置、固定代理。
这不是全模型、长期稳定性或正式客户迁移验收。

- 构建源码：`96079956d13753fb60629f3af5e9b901e5b5882d`；本机 Go 1.27.3，
  Linux amd64、CGO 关闭。服务版本 `local-lac03-acceptance-20261031`。
- 二进制 135,450,784 字节；本机和服务器 SHA-256 均为
  `0026210365f4d611e60f47b5a1ec72c94b415babc1c198c831cdd7c19de08049`。
- 独立服务 `newapi-acceptance-20261031.service`，已启用开机启动，
  仅监听 `127.0.0.1:18760`。通过工作站 SSH 映射访问
  `http://127.0.0.1:47860/v1`；未配置新的公网入口或域名。
- 独立 SQLite、管理员、验收用户和有限额 Key；支付未启用。
  登录和 Key 资料只保存于受保护的私有文件，不随源码上传。
- 候选自身限制为 1 CPU、768 MiB 内存上限、512 MiB 高水位、禁止使用 Swap，
  Nice 10。RS 的既有 cgroup 本层 CPU 和内存仍为 `max`。

### 真实调用与逐笔结算

RS 代表模型使用 `gpt-5.6-terra`，图片使用 `gpt-image-2.5`。
目录中其他模型和别名未逐个生成测试；没有调用 Cline 或其他上游。

| 场景 | 次数 | 结果 | 每次扣减单位 |
|---|---:|---|---:|
| Chat JSON | 1 | 正常答案 | 2,600 |
| Chat SSE | 1 | 正常答案、结束事件 | 2,600 |
| Responses JSON | 1 | 正常答案、completed | 2,600 |
| Responses SSE | 1 | 正常答案、completed | 4,600 |
| Chat 工具调用 | 1 | 工具名称和 JSON 参数正确 | 9,100 |
| Responses 工具调用 | 1 | 工具名称和 JSON 参数正确 | 9,100 |
| 图片生成 | 1 | PNG 完整解码，蓝色方块符合提示 | 1,000,000 |
| 图片编辑 | 1 | PNG 完整解码，保留方块并新增白色圆形 | 1,000,000 |
| 同一 WebSocket 两轮 Responses | 2 | 两轮完成、响应 ID 不同 | 2,600 |
| 无效 Key | 1 | HTTP 401，余额、Key 和消费流水不变 | 0 |

10 次成功调用分别对应 10 条消费流水。每次均比较客户端实际 usage、配置价格、
余额差额、Key 差额和流水金额；最终又独立查询累计账本和余额，结果一致：

```text
消费总额：2,035,800 单位 = 0.020358 USD（验收测试价）
测试余额：100,000,000 → 97,964,200
Key 额度：  5,000,000 →  2,964,200
用户累计消费 = Key 累计消费 = 消费流水合计 = 2,035,800
```

本次用独立的测试余额，未真实收款。文本价格为每百万 token：未缓存输入
1 USD、输出 2 USD、缓存输入 0.1 USD；图片为每次 0.01 USD；分组倍率为 1，
1 USD 对应 100,000,000 单位。这是可复算的验收配置，不代表现有商业售价已经迁移。
实测文本均未出现缓存命中，缓存非零的计算沿用此前合成测试证据。

图片生成约 87.62 秒，编辑约 145.80 秒，含客户端传输；文本四种普通调用约
2.47–3.56 秒。这些不是并发性能或相对 Sub2API 的延迟比较。
两个图片请求均指定 `1024x1024`，实际返回均为 `1254x1254`；图片语义和完整性
已检查，**未将精确输出 1024 像素尺寸记作通过**。

### 既有服务和回退边界

部署及验收后，原 Sub2API PID 和既有 LAC-03 容器 ID 保持，Sub2API 健康接口
正常。LAC-02 Chat2API 容器 ID、PID 和零重启状态保持，模型接口仍通过鉴权。
没有停止、重建或重启既有业务服务，没有主动断开其 HTTP、SSE 或 WebSocket。

最终 LAC-03 可用物理内存约 10.9 GiB，Swap 总量约 1 GiB、使用约 82.1 MiB，
与部署前相同；内存和 I/O 压力的 avg10/avg60 为零，候选无 OOM 或内存限额事件。
LAC-02 可用物理内存约 3.04 GiB，未配置 Swap。

回退只涉及此验收服务：先停止新增验收调用并等其自身请求自然结束，再停止或
禁用 `newapi-acceptance-20261031.service`。客户入口和原权威账本无须回切。
数据库、私有配置、当前制品和小型验收证据保留；本次没有为演练回退中断运行实例。

已删除明确计量的传输临时文件合计 104,739,621 字节（约 99.9 MiB），包括
服务器分段、服务器已消费归档及本机已消费归档。当前验收二进制约 129.2 MiB
保留以便恢复；源码、凭据、账本和真实图片证据保留。清理后本机磁盘约余
59.5 GiB，LAC-03 约余 72.7 GiB。

原始 `real-api-result.json`、`final-audit.json`、前后资源见证和真实图片保留于
工作站私有验收目录，不随公开源码提交。后续文档提交不改变上述实际运行二进制。
