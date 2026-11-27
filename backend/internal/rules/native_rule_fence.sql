-- Shared by the isolated Go collector and Rust scheduler. Bind the lower
-- legacy-vital timestamp, then device_id. All timestamps are UTC.
-- Only data that influences collectRuleInput/nextRuleTask is included.
-- Health history and future samples use the separate bounded delta proof;
-- legacy vitals, heartbeat and baseline replay keys have separate domains.
SELECT SHA2(CAST(JSON_ARRAY(
 d.device_id,d.old_id,d.health_epoch,d.status,d.device_no,
 CAST(d.last_time AS CHAR),CAST(d.bind_time AS CHAR),d.battery,
 p.revision,p.payload,w.hrf,w.hrl,w.bpf,w.bpl,
 t.epoch,t.eligible,CAST(t.due_at AS CHAR),
 (SELECT JSON_ARRAY(r.id,CAST(r.create_time AS CHAR),r.hr,r.bp,SHA2(r.measurement_cipher,256))
  FROM ratepre r WHERE r.device_id=d.device_id AND r.old_id=d.old_id AND r.create_time>=?
  ORDER BY r.create_time DESC,r.id DESC LIMIT 1),
 (SELECT s.observation_key FROM rule_state s WHERE s.device_id=d.device_id AND s.epoch=d.health_epoch AND s.kind='baseline_heart_rate'),
 (SELECT s.observation_key FROM rule_state s WHERE s.device_id=d.device_id AND s.epoch=d.health_epoch AND s.kind='baseline_blood_pressure'),
 EXISTS(SELECT 1 FROM care_incident i WHERE i.device_id=d.device_id AND i.old_id=d.old_id AND i.kind IN ('sos','fence_exit') AND i.state<>'verified')
) AS CHAR CHARACTER SET utf8mb4),256)
FROM device d
LEFT JOIN rule_policy p ON p.device_id=d.device_id
LEFT JOIN watch_info w ON w.device_id=d.device_id AND p.device_id IS NULL
LEFT JOIN rule_task t ON t.device_id=d.device_id
WHERE d.device_id=?
