-- Invalid database objects from V$OBJECTS (YashanDB catalog view)
-- Note: adapt to the actual catalog view name in your YashanDB version
SELECT OWNER, OBJECT_NAME, OBJECT_TYPE, STATUS, LAST_DDL_TIME
FROM V$OBJECTS
WHERE STATUS != 'VALID'
  AND OWNER NOT IN ('SYS', 'SYSTEM', 'YASDB', 'PUBLIC')
ORDER BY OWNER, OBJECT_TYPE, OBJECT_NAME;
