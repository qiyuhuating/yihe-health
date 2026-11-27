-- A reserved future range may begin above the last committed event.
ALTER TABLE health_owner_clock ADD COLUMN range_start BIGINT NOT NULL DEFAULT 0,
 ADD CONSTRAINT ck_health_owner_range_start CHECK(range_start>=0 AND range_start<=range_end);
