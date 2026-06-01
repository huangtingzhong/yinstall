-- Object counts by type from V$OBJECTS (YashanDB catalog view)
-- Note: adapt to the actual catalog view name in your YashanDB version
SELECT OBJECT_TYPE, COUNT(*) AS OBJ_COUNT
FROM V$OBJECTS
WHERE OWNER NOT IN ('SYS', 'SYSTEM', 'YASDB', 'PUBLIC')
  AND STATUS = 'VALID'
GROUP BY OBJECT_TYPE
ORDER BY OBJ_COUNT DESC;
