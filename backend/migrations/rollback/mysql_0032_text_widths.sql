-- 列宽放宽是向后兼容的扩展：只加宽、不缩小、不改类型，旧版本应用照常读写。
-- 回滚时保留已放宽的列宽，避免已录入的长文本被截断，只移除迁移记账。
SELECT name FROM schema_migrations WHERE name='mysql/0032_text_widths.sql';
