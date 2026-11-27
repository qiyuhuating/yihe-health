-- Offline migration: stop all application writers before allocating ranges.
-- A missing owner has never committed an event. Preserve existing cursors.
INSERT INTO health_owner_clock(old_id,revision,range_end)
 SELECT old_id,0,0 FROM oldperson
 WHERE NOT EXISTS (SELECT 1 FROM health_change WHERE health_change.old_id=oldperson.old_id)
 ON DUPLICATE KEY UPDATE old_id=health_owner_clock.old_id;

-- Materialized window results allocate disjoint future ranges. Both the global
-- watermark and every owner reservation change in one atomic InnoDB statement.
-- If the safe integer domain cannot hold the entire set, leave the old path.
UPDATE health_clock g
 JOIN (SELECT h.revision AS base FROM health_clock h CROSS JOIN (SELECT COALESCE(MAX(range_end),0) AS max_end FROM health_owner_clock) m WHERE h.id=1 AND h.revision>=m.max_end GROUP BY h.revision) b ON b.base=g.revision
 JOIN (SELECT c.old_id,ROW_NUMBER() OVER(ORDER BY c.old_id) AS ordinal,COUNT(*) OVER() AS total
       FROM health_owner_clock c JOIN oldperson o ON o.old_id=c.old_id
       WHERE c.range_end=c.revision) p
 JOIN health_owner_clock c ON c.old_id=p.old_id
 SET c.range_start=b.base+(p.ordinal-1)*65536,
     c.range_end=b.base+p.ordinal*65536,
     g.revision=b.base+p.total*65536
 WHERE g.id=1 AND c.range_end=c.revision AND g.revision>=c.range_end
 AND b.base<=9007199254740991-p.total*65536;
