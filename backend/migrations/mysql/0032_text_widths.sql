-- 放宽文本列宽：以下列宽不足以容纳真实录入内容，例如 room varchar(5) 装不下
-- 「A栋301室」（6 字符），在 STRICT_TRANS_TABLES 下会直接 1406 报错，最终表现为
-- 500「服务处理失败」。放宽后配合 handler 层的 vstrMax rune 校验，让超长输入
-- 得到明确的 400 而不是服务端错误。
--
-- 只做扩大，不缩小、不改类型，历史数据不受影响。
ALTER TABLE oldperson MODIFY name varchar(50) DEFAULT NULL;
ALTER TABLE oldperson MODIFY room varchar(50) DEFAULT NULL;
ALTER TABLE oldperson MODIFY illness varchar(200) DEFAULT NULL;
ALTER TABLE oldperson MODIFY warn varchar(50) DEFAULT NULL;
ALTER TABLE oldperson MODIFY food_like varchar(100) DEFAULT NULL;
ALTER TABLE oldperson MODIFY food_hate varchar(100) DEFAULT NULL;
ALTER TABLE oldperson MODIFY drag varchar(100) DEFAULT NULL;
ALTER TABLE family MODIFY family_name varchar(50) DEFAULT NULL;
ALTER TABLE family MODIFY family_address varchar(200) DEFAULT NULL;
ALTER TABLE customer MODIFY customer_name varchar(50) DEFAULT NULL;
ALTER TABLE customer MODIFY customer_address varchar(200) DEFAULT NULL;
ALTER TABLE said MODIFY person_name varchar(50) DEFAULT NULL;
