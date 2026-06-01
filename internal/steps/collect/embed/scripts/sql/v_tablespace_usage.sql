-- Tablespace info with datafile statistics (YashanDB V$ views)
-- V$TABLESPACE: tablespace metadata, ID used to join with V$DATAFILE.TS#
-- V$DATAFILE: BYTES (allocated), FREE_BLOCKS * BLOCK_SIZE (free)
SELECT t.ID, t.NAME, t.STATUS, t.CONTENTS, t.ALLOCATION_TYPE,
       ROUND(NVL(d.TOTAL_BYTES, 0) / 1024 / 1024, 2)              AS TOTAL_MB,
       ROUND(NVL(d.FREE_BYTES, 0) / 1024 / 1024, 2)               AS FREE_MB,
       ROUND((1 - NVL(d.FREE_BYTES, 0) / GREATEST(NVL(d.TOTAL_BYTES, 1), 1)) * 100, 2) AS USED_PCT
FROM V$TABLESPACE t
LEFT JOIN (
    SELECT TS#,
           SUM(BYTES)                        AS TOTAL_BYTES,
           SUM(FREE_BLOCKS * BLOCK_SIZE)     AS FREE_BYTES
    FROM V$DATAFILE
    GROUP BY TS#
) d ON d.TS# = t.ID
ORDER BY USED_PCT DESC NULLS LAST;
