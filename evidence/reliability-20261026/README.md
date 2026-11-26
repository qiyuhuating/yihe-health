# 证据索引与使用边界

每个结果绑定其 manifest/summary/source-sha。测试计数含子测试，不是独立业务用例数。
真实本机数据库与真实 Linux CI 分开记录；本机进程接收端不是外部人类值班渠道。

| 目录/文件 | 绑定与解释 |
|---|---|
| remote-baseline.json | 初始远端 preview 41b0f1d、PR 元数据。历史交接的“无当前 CI”不再适用于该新 preview：其三个 exact-head workflow 已成功 |
| baseline-50b9cc0-nvme | 50b9cc0 真 MySQL+PG/race：1041 pass、10 opt-in skip，build/vet 成功 |
| baseline-41b0f1d-nvme | 41b0f1d：1068 pass、10 opt-in skip，build/vet 成功 |
| candidate-acceptance-nvme | 2d0f308 的较早候选：1095 pass、10 skip；不替代后续代码的 CI |
| rollback-refinement-correct-db.txt | 使用实际 care_lab 基库重跑 rollback/reapply 成功 |
| ci-f1417a9-backend-1.26.10 | SHA f1417a9，Go 1.26.10，真实 MySQL 8.4.13/PG 16.15/race；每个主套件 1106 pass/10 opt-in skip；PG 专项 825 pass/42 解释过的 skip（MySQL-only 在配对套件必跑） |
| ci-f1417a9-frontend | 同 SHA 的 test/typecheck/build/npm audit 原始记录；21 tests，通过；audit 0 |
| verified-source-f1417a9.json | 实际在线运行 release-gate.py 校验此新分支的当时 head、最新 CI/Security 与 SHA256 artifact；不是永不过期发布许可 |
| process-matrix-110b5a4 | 修正告警归属与等待后，从干净 110b5a4 构建/运行；以 matrix.json.complete 和逐项记录为准，真实租约和升级 deadline |
| process-matrix-110b5a4/final-audit-timeline.json | 演练后只读恢复同一测试实例检查 audit；ack 后 614.111 秒真实升级，无任务状态改写；之后停掉独立实验数据库服务，保留数据 |
| promtool-check.txt | 正式规则检查记录 |
| schema-bf33177.tsv | 只含 information_schema 元数据，不含业务行；385 字段分类输入 |
| python-gates-and-watchdog.txt | 5 个发布失败关闭测试 + 2 个 watchdog 持久重试/顺序与接收端边界测试 |

原始凭据、TLS 私钥、fixture session、SQL dump 和 S3 数据均未放入此目录。截图不能代替原始
日志。运行日志中的测试姓名/号码来自合成 fixture。本目录仅保留历史记录，不代表当前提交已通过验证。
清理前的完整记录可从 Git 历史查阅；较早 NVMe 候选仅保留 summary。
