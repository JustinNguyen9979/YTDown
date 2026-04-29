set -e

BASE_DATE=$(date +"%-Y.%-m.%-d")

if git rev-parse "$BASE_DATE" >/dev/null 2>&1; then
  PATCH=1
  while git rev-parse "$BASE_DATE.$PATCH" >/dev/null 2>&1; do
    PATCH=$((PATCH + 1))
  done
  VERSION="$BASE_DATE.$PATCH"
else
  VERSION="$BASE_DATE"
fi

echo "$VERSION"

# Xuất ra GitHub Actions nếu đang chạy trong CI
if [ -n "$GITHUB_OUTPUT" ]; then
  echo "version=$VERSION" >> "$GITHUB_OUTPUT"
fi