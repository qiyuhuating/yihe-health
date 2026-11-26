-- 位置坐标（老人轨迹/最新位置/告警坐标）改为 securestore 密文落库（PII 敏感信息）。
-- 列宽从 varchar(20) 扩到 varchar(128) 以容纳 "sne1:" + base64 密文；
-- 历史明文行由读取端兼容（无前缀原样返回），无需数据回填。
-- fence_event.lng/lat 是 double 数值列且按数字消费，保持明文。
ALTER TABLE device_location MODIFY lng varchar(128) DEFAULT NULL, MODIFY lat varchar(128) DEFAULT NULL;
ALTER TABLE device MODIFY last_lng varchar(128) DEFAULT NULL, MODIFY last_lat varchar(128) DEFAULT NULL;
ALTER TABLE alarm_precess MODIFY lng varchar(128) DEFAULT NULL, MODIFY lat varchar(128) DEFAULT NULL;
ALTER TABLE alarm_process MODIFY lng varchar(128) DEFAULT NULL, MODIFY lat varchar(128) DEFAULT NULL;
