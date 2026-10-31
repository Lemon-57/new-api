# LAC-03 isolated API acceptance

Scope: deploy an independent NewAPI candidate on LAC-03 and verify real RS and
Chat2API inference with correct wallet, API key and usage-log settlement.
Do not switch customer traffic, change production account state or proxies,
or migrate existing balances and databases.

The application supports `BIND_ADDRESS`, defaulting to its existing wildcard
listen address. Set `BIND_ADDRESS=127.0.0.1` for the acceptance service. Access it
through an authenticated SSH local forward for initial isolation. The later
user-authorized public domain is documented below. Build locally and verify SHA-256 both
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

## 公网域名入口（后续更新）

已按用户指定接入 **https://newapi.killai.cc**；API Base URL 为
**https://newapi.killai.cc/v1**，登录页为 **https://newapi.killai.cc/sign-in**。
账号、密码、API Key、数据库和模型映射沿用原验收实例。

- 仅新增该名称的 DNS-only A 记录，指向 LAC-03 104.250.109.133，TTL 60。
  Cloudflare 和 Google 公共 DNS 均返回正确地址；没有添加 AAAA 或修改其他业务记录。
- 工作站验证了公开受信证书链、域名 SAN 及 TLS 1.3。签发者为 Let's Encrypt，
  初次证书到期时间为 2027-01-29 14:48:09 UTC。证书由 Caddy 自动管理；
  HTTP 返回 308 并跳转 HTTPS。
- 应用继续只监听回环端口。共享入口只新增域名路由和证书策略，原路由、设置、
  TLS 策略逐项比对保持；更新使用 ETag / If-Match，原网关进程没有重启。
  持久配置在原文件完整内容后追加该站点。
- 应用配置前连续三次未见活动 Upgrade/WebSocket 处理栈，既有 HTTP/SSE 按
  网关机制继续处理。新域名沿用 24 小时 stream_close_delay：
  它延迟配置卸载时对 WebSocket 的关闭，不代表无限保留，也不设连接总时长限制。
- ServerAddress 已设置为该 HTTPS 域名，SESSION_COOKIE_SECURE=true，
  SESSION_COOKIE_TRUSTED_URL 仅包含 https://newapi.killai.cc。
  五次连接检查确认验收实例空闲后，只重载该实例应用安全设置，余额与账本保持。
  原 Sub2API、RS、Chat2API 和共享网关没有重启。

| 公网检查 | 结果 |
|---|---|
| HTTPS 登录、正常来源刷新、测试会话退出 | HTTP 200 |
| 认证刷新 Cookie | Secure、HttpOnly、SameSite=Strict |
| 非可信 Origin 刷新 | HTTP 403 |
| 原 API Key 查询模型 | HTTP 200，共 13 个配置模型 |
| HTTPS JSON、HTTPS SSE | 正常答案，每次一条消费流水 |
| 同连接两轮 WSS | 两轮完成，响应 ID 不同，每轮一条消费流水 |
| 无效 Key | HTTP 401，未扣费 |
| 原 api / apilax 域名健康检查 | HTTP 200，status=ok |

四次新增推理各扣 2,400 单位，合计 0.000096 USD（既有测试价）。
累计 14 条消费流水、2,045,400 单位，余额剩 97,954,600，Key 剩 2,954,600；
余额、Key、实际 usage 计算和流水逐笔及累计一致。未重复生成图片，
不把本次域名检查概括为全部模型或长期负载验证。

工作站解析曾间歇失败。测试保留原主机名和完整证书校验，仅在该域名解析失败时
在进程内使用已核实的目标 IP；未修改 hosts、系统 DNS 或关闭证书校验。
公共解析另经两个外部 DNS 服务核实。

热加载和流式窗口参照 [Caddy API](https://caddyserver.com/docs/api)、
[reverse_proxy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy) 文档；
记录操作参照 [Cloudflare DNS API](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/)。
完整网关配置、DNS 凭据、证书私钥、会话 Cookie 和登录记录不随源码上传。


## 双入口倍率计费（2026-11-01）

现网对照关系已按实际配置核实：

| 入口 | 入口角色 | 普通文本倍率 | 图片倍率 |
|---|---|---:|---:|
| `api.killai.cc` | optimized | 0.22 | 1 |
| `apilax.killai.cc` | ordinary | 0.20 | 1 |
| `newapi.killai.cc` | optimized | 0.22 | 1 |
| `newapilax.killai.cc` | ordinary | 0.20 | 1 |

`newapi.killai.cc` 和 `newapilax.killai.cc` 共用现有验收实例的 Key、余额和账本。NewAPI 通过受信边缘写入一次性入口证明；后端只接受来自 loopback 边缘的固定随机令牌，并删除客户端同名请求头。客户端自带 Host、X-Forwarded 头、错误令牌、重复头或未知角色都会在访问上游前返回 403。入口倍率进入现有计费快照，沿用原表达式和实际 usage，结算不重复扣费。

真实验证结果：

- 候选进程在 LAC-03 以独立端口 `18761` 启动，共享原验收 SQLite 账本；原旧实例和客户端路由未停止。新请求已切到候选，旧实例保留自然排空。
- loopback 候选的 ordinary 计费为 540 单位，optimized 为 594 单位；同一 usage、`0.2 / 0.22` 倍率和流水读回一致。图片别名走独立 1 倍。
- 公网域名 JSON/SSE 均 HTTP 200 并返回完整答案；账本逐笔读回：`newapi.killai.cc` 记录 `billing_ingress=optimized`、`group_ratio=0.22`，`newapilax.killai.cc` 记录 `billing_ingress=ordinary`、`group_ratio=0.20`。
- WebSocket 每轮都从初始认证上下文复制入口证明，不依赖伪造的后续请求头；回环测试覆盖两轮新上下文、重复结算和拒绝伪造入口。
- 优化入口所在 SJC 的 Nginx 只追加 HTTP ACME 站点和 HTTPS 站点证书自动化所需的候选配置；加载前检查无活动 Upgrade 栈，Nginx master PID、容器 ID、重启次数不变。LAC-03 Caddy 同样保持原进程，通过已有自动 HTTPS 机制取得 `newapilax.killai.cc` 证书。没有远程编译。

倍率字段只改变入口对应的 group multiplier；NewAPI 当前模型基础表达式、图片独立价格、用户余额、Key 额度和账本单位均保持不变。订阅额度兑换倍率不在这次入口计费范围内。现有账本已含此前验收和本次公网调用；新增入口证据保留于本机私有目录，凭据、入口令牌、证书私钥和原始配置未进入 Git。
