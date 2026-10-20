# Agent Webhook 静态 Authorization 头如何穿过 safefetch 边界

## Status
Accepted
Class: architecture

## Context and Problem Statement

部分接收端（例如 routine/automation webhook）只用静态 `Authorization` 头鉴别调用方，无法校验 Standard Webhooks 签名，因此接不上 Agent Webhook 投递（issue #1203）。投递的唯一发送通道是 `safefetch.Client.PostJSON`，它有意只从调用方传入的头表拷贝五个 `Webhook-*` 头，其余头一律剥离——这是"任何 ambient 凭证都不上线"的既有保证（回归测试曾以 `never-forward-this` 断言该剥离）。需要决定：接收端所需的静态凭证如何跨过这条边界，同时不削弱该保证。

## Decision Drivers

- 只有调用方显式构造的凭证可以上线；环境、代理等 ambient 凭证必须继续无法到达接收端。
- safefetch 的既有保证不因该扩展而改变：请求由 `PostJSON` 全新构造、`transport.Proxy = nil`、禁止跟随重定向，其余调用方头继续剥离。
- 凭证按 Agent 隔离：落库前校验可见 ASCII（可含空格/制表符，禁 CR/LF），用独立 securestore purpose 加密，接口只回显"是否已配置"。
- 保持单一发送通道，避免为单个头维护第二条发送路径（URL/DNS 校验、体积、超时、并发策略会在两条路径上漂移）。

## Considered Options

- 在 `PostJSON` 的头白名单中显式加入 `Authorization`（本决策）。
- 新增带凭证的发送 API（如 `PostJSONAuthorized`），`PostJSON` 保持一律剥离。
- 允许每个 Agent 配置任意自定义头（如 `X-API-Key`）。

## Decision Outcome

选择扩展白名单：`forwardedHeaderNames`（`apps/gooseforum/app/bundles/safefetch/safefetch.go`）是唯一转发清单，`Authorization` 仅在调用方显式置入头表时转发；未列入清单的调用方头（例如 `X-Ambient-Credential`）继续被丢弃。防 ambient 泄漏不依赖清单长度：`PostJSON` 从零构造请求，头只能来自调用方显式构造的头表，而重定向与代理已分别被拒绝。

当前唯一调用方是 Agent Webhook 投递：`deliveryHeaders` 在配置了静态头时设置 `Authorization`，五个 `Webhook-*` 头与 HMAC 签名（[0069](0069-agent-interaction-events.md)）保持不变。该值是接收端的 bearer 凭证，泄露时在接收端轮换并重新录入；密文无法解密（例如 `app.signingKey` 变更）时投递不发送、Agent 以 `authorization_invalid` 暂停（恢复步骤见 [Agent 运维文档](../operations/agents.md#optional-static-authorization-header)）。

回归证据位于 safefetch 的线级用例（真实 TLS 出站）：显式 `Authorization` 到达接收端、同批非白名单头被剥离、未显式配置时不上线；配置、加密与不落非法值由 agentwebhookservice 与 route 级用例覆盖。

## Pros and Cons of the Options

- 扩展白名单（选定）：改动最小、保持单一发送通道、白名单仍然显式；代价是 `PostJSON` 的契约从"仅 `Webhook-*`"扩为"含 `Authorization`"，未来调用方若把 `Authorization` 放进头表就会转发（缓解：头必须由调用方显式构造，清单与测试固定该语义）。
- 新增带凭证的发送 API：`PostJSON` 语义完全不变；代价是两条发送路径的 URL/DNS/重定向/体积/并发策略需要长期同步，而当前只有一个消费者，重复面大于收益。若未来出现第二个需要不同凭证形态的调用方，可改为在其上分层。
- 任意自定义头：可覆盖 `X-API-Key` 等场景；代价是保留头覆盖、头注入、头数量与长度限制的额外面，超出 #1203 收窄后的范围，需要时可在本决策之上扩展。

## Links

- [用户反馈 #1203](https://github.com/YourTongji/YourTJ-Hub/issues/1203)
- [PR #1204](https://github.com/YourTongji/YourTJ-Hub/pull/1204)
- [Agent 运维：可选静态 Authorization 头](../operations/agents.md#optional-static-authorization-header)
- [0069 Durable public interactions for Agent personas](0069-agent-interaction-events.md)
