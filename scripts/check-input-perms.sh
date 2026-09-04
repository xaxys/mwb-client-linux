#!/bin/sh
# input-group self-check (evdev/uinput restricted mode helper).
# usage: sh scripts/check-input-perms.sh
set -eu
ok=1
if [ -e /dev/uinput ]; then
  if [ -r /dev/uinput ] && [ -w /dev/uinput ]; then
    echo "uinput: OK (/dev/uinput rw)"
  else
    echo "uinput: PRESENT but not rw — add yourself to 'input' group:"
    echo "  sudo usermod -aG input $USER   # then re-login"
    ok=0
  fi
else
  echo "uinput: MISSING (/dev/uinput) — modprobe uinput or install udev rules"
  ok=0
fi
if groups 2>/dev/null | grep -qw input; then
  echo "group: OK (input)"
else
  echo "group: MISSING (not in 'input') — see above"
  ok=0
fi
echo "session: XDG_SESSION_TYPE=${XDG_SESSION_TYPE:-?} XDG_CURRENT_DESKTOP=${XDG_CURRENT_DESKTOP:-?}"
exit $ok
