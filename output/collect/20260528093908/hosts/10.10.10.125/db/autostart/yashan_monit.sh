#!/bin/bash
if [ $# -eq 0 ]; then
    echo "Usage: $0 <PORT> or $0 bashrc"
    echo "Example: $0 1788"
    echo "Example: $0 bashrc"
    exit 1
fi

MONIT_AUTOSTART="true"                
YCSROOTAGENT_AUTOSTART="true"
YASDB_USER=yashan                              
INTERVAL=3                              
PORT=$1

# Determine environment file based on PORT argument
if [ "$PORT" = "bashrc" ]; then
    ENV_FILE="/home/${YASDB_USER}/.bashrc"
else
    ENV_FILE="/home/${YASDB_USER}/.${PORT}"
fi

if [ -f "$ENV_FILE" ]; then
    source "$ENV_FILE"
else
    echo "$(date) Error: Environment file $ENV_FILE not found"
    exit 1
fi

if [ -z "$YASDB_HOME" ]; then
    echo "$(date) Error: YASDB_HOME is not set. Please check $ENV_FILE"
    exit 1
fi

export LD_LIBRARY_PATH=$YASDB_HOME/lib

MONITRC_FILE="$YASDB_HOME/om/monit/monitrc"
if [ -f "$MONITRC_FILE" ]; then
    CURRENT_OWNER=$(stat -c '%U' "$MONITRC_FILE" 2>/dev/null || stat -f '%Su' "$MONITRC_FILE" 2>/dev/null)
    if [ "$CURRENT_OWNER" != "$YASDB_USER" ]; then
        echo "$(date) Fixing monitrc owner: $CURRENT_OWNER -> $YASDB_USER"
        chown $YASDB_USER "$MONITRC_FILE"
    fi
    
    CURRENT_PERM=$(stat -c '%a' "$MONITRC_FILE" 2>/dev/null)
    if [ -z "$CURRENT_PERM" ]; then
        PERM_STR=$(ls -ld "$MONITRC_FILE" 2>/dev/null | awk '{print $1}')
        if [ -z "$PERM_STR" ]; then
            CURRENT_PERM="000"
        else
            CURRENT_PERM="000"
        fi
    fi
    if [ "$CURRENT_PERM" != "700" ]; then
        echo "$(date) Fixing monitrc permissions: $CURRENT_PERM -> 700"
        chmod 0700 "$MONITRC_FILE"
    fi
else
    echo "$(date) Warning: $MONITRC_FILE not found"
    exit 1
fi

while true; do    
    if [ "$MONIT_AUTOSTART" = "true" ]; then
        if ! pgrep -a monit | grep "$YASDB_HOME" > /dev/null; then
            echo "$(date) monit abnormal, try restart..." 
            if [ "$PORT" = "bashrc" ]; then
                su - $YASDB_USER -c "source /home/${YASDB_USER}/.bashrc && $YASDB_HOME/om/bin/monit -c $YASDB_HOME/om/monit/monitrc" &
            else
                su - $YASDB_USER -c "source /home/${YASDB_USER}/.${PORT} && $YASDB_HOME/om/bin/monit -c $YASDB_HOME/om/monit/monitrc" &
            fi
        fi
    fi
    if [ "$YCSROOTAGENT_AUTOSTART" = "true" ]; then
       if [ -n "$YASCS_HOME" ]; then
         if ! pgrep -a ycsrootagent | grep "$YASCS_HOME" > /dev/null; then
           echo "$(date) ycsrootagent abnormal, try restart..."
           if [ "$PORT" = "bashrc" ]; then
               su - $YASDB_USER -c "source /home/${YASDB_USER}/.bashrc && sudo env LD_LIBRARY_PATH=$YASDB_HOME/lib $YASDB_HOME/bin/ycsrootagent start -H $YASCS_HOME" & 
           else
               su - $YASDB_USER -c "source /home/${YASDB_USER}/.${PORT} && sudo env LD_LIBRARY_PATH=$YASDB_HOME/lib $YASDB_HOME/bin/ycsrootagent start -H $YASCS_HOME" & 
           fi
         fi
       fi
    fi
    sleep "$INTERVAL"
done

