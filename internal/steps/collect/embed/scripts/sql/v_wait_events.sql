-- System-level wait events (all categories, ordered by time waited)
-- V$SYSTEM_EVENT tracks cumulative waits since instance startup
SELECT EVENT, WAIT_CLASS, TOTAL_WAITS, TOTAL_TIMEOUTS,
       TIME_WAITED, AVERAGE_WAIT
FROM V$SYSTEM_EVENT
ORDER BY TIME_WAITED DESC;
