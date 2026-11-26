-- 回滚坐标加密：恢复原列宽。仅当表中尚无密文行（>20 字符）时安全，
-- 否则 MySQL 严格模式下 MODIFY 会因数据截断而失败。
ALTER TABLE device_location MODIFY lng varchar(20) DEFAULT NULL, MODIFY lat varchar(20) DEFAULT NULL;
ALTER TABLE device MODIFY last_lng varchar(20) DEFAULT NULL, MODIFY last_lat varchar(20) DEFAULT NULL;
ALTER TABLE alarm_precess MODIFY lng varchar(20) DEFAULT NULL, MODIFY lat varchar(20) DEFAULT NULL;
ALTER TABLE alarm_process MODIFY lng varchar(20) DEFAULT NULL, MODIFY lat varchar(20) DEFAULT NULL;
DELETE FROM schema_migrations WHERE name='mysql/0011_encrypt_coordinates.sql';
