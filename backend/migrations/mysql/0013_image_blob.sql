CREATE TABLE IF NOT EXISTS image_blob (
 name varchar(255) PRIMARY KEY,
 content_sha256 char(64) NOT NULL,
 size_bytes bigint NOT NULL,
 state varchar(16) NOT NULL DEFAULT 'ready',
 created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX idx_image_blob_gc(state,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS image_gc_guard (
 id integer PRIMARY KEY,
 backup_owner varchar(64) NOT NULL DEFAULT '',
 backup_until datetime(6) NOT NULL
) ENGINE=InnoDB;
INSERT IGNORE INTO image_gc_guard(id,backup_owner,backup_until) VALUES(1,'','1970-01-01');
