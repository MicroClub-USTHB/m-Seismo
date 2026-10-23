#!/usr/bin/env bash
# Runs every API test script in order and reports a combined result.
# The scripts share the state file, so the owner, bearer token and GitHub
# connection created by the first are reused (and re-verified) by the rest.
#
#   backend/scripts/api/all.sh            # reuse the saved state
#   backend/scripts/api/all.sh --fresh    # start with a new owner and connection
#
# Exit code: 0 when every script passed.
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_FILE="${STATE_FILE:-$DIR/.env.test}"
export STATE_FILE

if [[ ${1-} == --fresh ]]; then
  rm -f "$STATE_FILE"
  echo "removed $STATE_FILE"
fi

scripts=(auth owners projects apps providers routing issues gitlab bitbucket)
failed=()

for name in "${scripts[@]}"; do
  printf '\n\033[1m########## %s ##########\033[0m\n' "$name"
  if ! bash "$DIR/$name.sh"; then
    failed+=("$name")
  fi
done

printf '\n\033[1m========== overall ==========\033[0m\n'
if [[ ${#failed[@]} -eq 0 ]]; then
  printf '\033[32mall %d scripts passed\033[0m\n' "${#scripts[@]}"
else
  printf '\033[31m%d of %d scripts failed: %s\033[0m\n' "${#failed[@]}" "${#scripts[@]}" "${failed[*]}"
  exit 1
fi
