#!/bin/sh
# Start the backend as the unprivileged "app" user.
#
# The container starts as root only long enough to fix ownership of the
# uploads volume: volumes created before the backend stopped running as root
# hold root-owned photos, and restore.sh copies photos in with "docker cp",
# which does not set an owner the app can use. Only files with the wrong
# owner are touched, so this is quick on a volume that is already right.
set -e

if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/uploads
    find /app/uploads \( ! -user app -o ! -group app \) -exec chown app:app {} +
    exec su-exec app:app "$@"
fi

exec "$@"
