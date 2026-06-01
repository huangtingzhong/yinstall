-- Redo log files and their status from V$LOG (V$LOG_HISTORY may not be available)
-- Provides a snapshot of redo log group status (CURRENT/ACTIVE/INACTIVE/UNUSED)
SELECT GROUP#, MEMBERS, STATUS, ARCHIVED, FIRST_CHANGE#, FIRST_TIME
FROM V$LOG
ORDER BY FIRST_TIME DESC;
