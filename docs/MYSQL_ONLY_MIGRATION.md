# MySQL-only Architecture & Migration

The new backend keeps **MySQL 8.4 as the only authoritative and persistent application datastore**. It does not connect to PostgreSQL, S3, a local image directory or Redis. Browser cache, short-lived service memory, HarmonyOS encrypted offline upload queues, Prometheus metrics and operating-system files used to start the application are not alternative business databases.

## Scope and compatibility

- Preserve every existing `/api/v1/*` endpoint, the Go SOS/care workflow, device authentication, sessions, staff permissions, alert rules, telemetry, and the Vue/Tauri/HarmonyOS clients.
- Existing MySQL business tables remain authoritative: accounts (`users`, `family`), residents (`oldperson`), devices (`device`), healthcare and telemetry (`ratepre`, `watch_day_info`, `device_location`), care (`care_incident`, `care_incident_event`) and immutable `image_blob` catalog.
- **New migration 0033** introduces `image_payload`: `name VARCHAR(255)` primary key, `payload MEDIUMBLOB NOT NULL` (authenticated ciphertext), `created_at DATETIME(6)`, and age index. The catalog keeps plaintext SHA-256, size, state, creation date and reference/GC locks. Application API never returns encrypted bytes.
- **New migration 0034** retires fifteen PostgreSQL analytics-outbox triggers. The historical MySQL `analytics_outbox` and `analytics_source` tables are deliberately retained **read-only**, without a worker or new producers, to avoid destroying existing audit/export material. They can be dropped only after separate archival approval.
- Existing statistics are computed by MySQL business queries; the legacy `analytics.configured`, `analytics.ready`, `analytics.pending` response keys remain for frontend compatibility and now report same-MySQL readiness, with pending = 0. They do **not** signify a second analytics database.
- `BLOB_BACKEND=local`, `BLOB_BACKEND=s3` and nonempty `PG_ANALYTICS_URL` are rejected with an actionable configuration error. New/default `BLOB_BACKEND=mysql`. `IMAGE_DIR` and S3 credentials are no longer runtime application settings.
- `DATA_ENCRYPTION_KEY` and any rotation keyring remain necessary; do not rotate during migration.
- **Risk audit recovery caveat:** The read-only production container writes a bounded risk-audit retry journal to `/tmp/yihe-risk-audit.jsonl` on `tmpfs`; this is **not durable across container recreation**. The authoritative audit history is in MySQL. If MySQL remains down during an abrupt container crash, uncommitted audit attempts can be lost. Preserve this as a release risk until a fail-closed/synchronous audit guarantee is demonstrated; do not add a second durable storage backend merely to conceal it.

## Required production migration (stop all old writers)

1. **Stop the old service and every writer**, including background workers, before taking the cutover backups.
2. **Back up** the quiesced MySQL schema and data, including `schema_migrations`, with `mysqldump --single-transaction --routines --triggers --hex-blob`. Preserve the encryption keyring and the original image storage or a complete private S3 export with identical object names. Retain the original images and PostgreSQL volume.
3. In the new checkout, set the unchanged `DATA_ENCRYPTION_KEY` and `BLOB_BACKEND=mysql`; remove `PG_ANALYTICS_URL`. Run `go run ./cmd/import-images --source /path/to/export` from the backend directory. This checks source readability and image key authentication offline. The apply step checks database completeness.
4. Set `MYSQL_DSN` to the migration target and run `go run ./cmd/import-images --source /path/to/export --apply`. The tool runs checkpointed migrations and copies plaintext-or-encrypted legacy objects into authenticated `image_payload` rows. It compares hashes and existing catalog metadata, refuses collisions and different bytes, and is restartable/idempotent. No source files are deleted.
5. A zero `missing_references` count and successful full `VerifyReferences` are required. This count covers active image references and catalog entries marked ready. Starting the API independently also checks referenced MySQL image rows before publishing business routes; missing migrated images cause a visible 503 rather than serving broken references.
6. Start `python scripts/start-platform.py` for the single-MySQL production Compose stack (or launch the Go binary directly with its MySQL DSN). Restore session/OS/runtime configuration and run the checks below before allowing business writes.

**New installations** have no legacy objects: deploy MySQL and let the Go service apply the embedded migrations before accepting requests.

## Validation and rollback

Run the exact-source Go race suite, `go build ./...`, `go vet ./...`, the frontend tests, the 62 HarmonyOS protocol/synchronizer simulations, and native Windows/HarmonyOS acceptance separately. Integration tests must use a disposable real MySQL 8.4 with `TEST_MYSQL_DSN`.

Check on the migrated database:

```sql
SELECT COUNT(*) AS stored_images FROM image_payload;
SELECT COUNT(*) AS old_events_retained FROM analytics_outbox;
SELECT COUNT(*) AS obsolete_analytics_triggers
  FROM information_schema.TRIGGERS
  WHERE TRIGGER_SCHEMA = DATABASE() AND TRIGGER_NAME LIKE 'yihe_analytics_%';
SELECT name, size_bytes, content_sha256, state FROM image_blob ORDER BY name LIMIT 20;
```

The last trigger count must be **0**, and referenced objects must pass full authenticated SHA-256 verification. Verify original and restored MySQL row counts, binding/SOS workflow and transaction rollback, backup/restore of large `MEDIUMBLOB` rows, user login, image upload/download, and database restart. An optional local image backup is a **migration input**, never a runtime fallback.

**Rollback requires restoring the complete pre-upgrade MySQL backup**, including the old migration ledger and triggers, along with the original image storage. After the new service has accepted writes, reconcile those writes against the rollback target before restoring the old backup. The migration ledger rejects old binaries started against an upgraded schema. Do not claim production readiness until native and real-data recovery gates pass.
## 隐私迁移编号衔接

本分支保留上游 `0033_mysql_image_payload.sql` 和 `0034_retire_pg_projection.sql`，
社交手机号、长者照护档案和监护人联系方式分别使用 `0035`、`0036`、`0037`。
上述三项仍需独立预检、停写、匹配备份及显式回填，不会随 schema 迁移自动加密历史行。

若已执行未合并 fork 的 `0033_social_phone_protection.sql`、
`0034_resident_care_profile_protection.sql` 或 `0035_guardian_contact_protection.sql`，
不得直接升级或手改 ledger 来冒充新编号。启动迁移在任何 DDL 前检查完成 ledger 和
statement journal，未知名称立即拒绝，不清除 checksum、schema fingerprint 或未完成 intent。
先保留原库/密钥/镜像及匹配的可恢复备份，继续使用匹配版本；升级须另行审核 schema、
回填状态、历史 SQL checksum 和全部指纹后实施显式迁移。本 PR 不提供自动 ledger 重命名。
已知编号的合法 rollback/reapply 仍遵循原检查点流程。

单 MySQL 验收包含加密图片 payload、目录和引用一致性。历史 PG、S3、本地图片、WAL、
dump 和旧密钥仍须保留在恢复资产清单；移除在线组件不代表已迁移、可删除或已完成密钥退役。
