-- Bind current revision, device, owner, previous revision, current revision.
-- Owner ranges are sparse: count alone cannot establish continuity.
-- This proof relies on normal append-only health_change and immutable
-- sample identity (owner/device/epoch/source/record/metric). Corrections
-- and tombstones retain their projection row and receive a new revision.
-- Include all epochs: Go baseline selection spans historical bindings.
-- Cap work at 257 journal entries; overflow or missing data is a conflict.
SELECT CASE WHEN COUNT(*) BETWEEN 1 AND 256
 AND MAX(delta.revision)=?
 AND COALESCE(SUM(CASE WHEN h.sample_id IS NULL OR h.old_id<>delta.old_id
 OR (h.device_id=? AND h.metric IN ('heart_rate','blood_pressure_systolic','steps'))
 THEN 1 ELSE 0 END),0)=0 THEN 1 ELSE 0 END
FROM (
 SELECT revision,old_id,sample_id FROM health_change
 WHERE old_id=? AND revision>? AND revision<=?
 ORDER BY revision LIMIT 257
) delta
LEFT JOIN health_sample h ON h.sample_id=delta.sample_id
