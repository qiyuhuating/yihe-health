# 环境验证与回滚边界

## 可重复验证

验证必须在独立 clone 和可销毁的 MySQL 8.4 测试数据库中执行。不要对生产库运行自动测试、故障注入或迁移演练；不要停止、清空或覆盖部署方现有数据库服务。

后端在 `backend` 目录执行：

```sh
go test -count=1 ./...
go test -race -count=1 -timeout 600s ./...
go build ./...
go vet ./...
```

需要真实数据库集成测试时，配置专用的 `TEST_MYSQL_DSN`。测试库必须允许测试初始化和销毁；未配置时跳过的测试不能算作数据库验收通过。

前端在 `frontend` 目录执行 `npm ci`、`npm test`、`npm run typecheck` 和 `npm run build`；具体脚本以当前 `package.json` 为准。仓库根目录执行 `python scripts/generate-openapi.py --check`。HarmonyOS 端执行 `npm ci` 与 `npm test`；便携协议测试不等价于 DevEco 原生 SDK 编译或真机验收。

CI 结果必须对应被审查的完整 commit SHA。每项检查按实际状态记录；跳过、blocked、未配置服务和未连接真实设备均不得写成通过。环境版本、数据库镜像和测试数据来源应从该次 CI artifact 或运行日志读取，不复用历史实验版本号作为当前环境声明。

## Git 回滚

未合并的预览改动应通过正常 revert 或修复提交回退，不要对共享分支执行 `reset --hard` 或 force push。合并后回退前，先确认当前分支、提交范围和是否已有新写入；保留审计记录，不要删除其他协作者的提交。

## 数据库与图片回滚

应用回滚不等于数据库回滚。切换旧二进制前，必须确认其支持当前 schema、图片 BLOB 格式和加密 keyring；不能让旧程序读取不兼容的新字段或密文。数据库结构回退必须按对应 migration 的 rollback 脚本评估，并在隔离副本验证后执行。

生产恢复前先停止写入者与通知发送者，保留当前数据库（含图片 BLOB）和密钥备份；恢复到隔离 MySQL，核对迁移版本、照护事件、审计、outbox、图片引用及内容哈希。新主启用前还要核对 SOS `requestId` 与网关幂等状态，防止重复联系真实人员。详细的协调备份与恢复步骤见 [备份与灾难恢复](../runbooks/backup-restore.md)。

## 尚需独立生产验收

真实手表与传感器、外部通知送达、独立人员接手和二人复核、异机寻呼、整机失效、生产体量恢复及 RPO/RTO、最旧密钥恢复、明文历史数据迁移、多 ingress 全局限额和受保护分支管理员规则，均须在对应生产环境单独验收。模拟 actor、回环故障代理和 CI 通过不能替代真人救援演练。
